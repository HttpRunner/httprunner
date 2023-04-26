package uixt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Driver struct {
	urlPrefix *url.URL
	sessionId string
	client    *http.Client
	// cache the last launched package name
	lastLaunchedPackageName string
}

// HTTPClient is the default client to use to communicate with the WebDriver server.
var HTTPClient = http.DefaultClient

type RawResponse []byte

var uia2Header = map[string]string{
	"Content-Type": "application/json;charset=UTF-8",
	"accept":       "application/json",
}

func (wd *Driver) concatURL(u *url.URL, elem ...string) string {
	var tmp *url.URL
	if u == nil {
		u = wd.urlPrefix
	}
	tmp, _ = url.Parse(u.String())
	tmp.Path = path.Join(append([]string{u.Path}, elem...)...)
	return tmp.String()
}

func (wd *Driver) httpGET(pathElem ...string) (rawResp rawResponse, err error) {
	return wd.httpRequest(http.MethodGet, wd.concatURL(nil, pathElem...), nil)
}

func (wd *Driver) httpPOST(data interface{}, pathElem ...string) (rawResp rawResponse, err error) {
	var bsJSON []byte = nil
	if data != nil {
		if bsJSON, err = json.Marshal(data); err != nil {
			return nil, err
		}
	}
	return wd.httpRequest(http.MethodPost, wd.concatURL(nil, pathElem...), bsJSON)
}

func (wd *Driver) httpDELETE(pathElem ...string) (rawResp rawResponse, err error) {
	return wd.httpRequest(http.MethodDelete, wd.concatURL(nil, pathElem...), nil)
}

func (wd *Driver) httpRequest(method string, rawURL string, rawBody []byte) (rawResp rawResponse, err error) {
	log.Debug().Str("method", method).Str("url", rawURL).Str("body", string(rawBody)).Msg("request driver agent")

	var req *http.Request
	if req, err = http.NewRequest(method, rawURL, bytes.NewBuffer(rawBody)); err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	var resp *http.Response
	if resp, err = wd.client.Do(req); err != nil {
		return nil, err
	}
	defer func() {
		// https://github.com/etcd-io/etcd/blob/v3.3.25/pkg/httputil/httputil.go#L16-L22
		_, _ = io.Copy(ioutil.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	rawResp, err = ioutil.ReadAll(resp.Body)
	logger := log.Debug().Int("statusCode", resp.StatusCode).Str("duration", time.Since(start).String())
	if !strings.HasSuffix(rawURL, "screenshot") {
		// avoid printing screenshot data
		logger.Str("response", string(rawResp))
	}
	logger.Msg("get driver agent response")
	if err != nil {
		return nil, err
	}

	if err = rawResp.checkErr(); err != nil {
		if resp.StatusCode == http.StatusOK {
			return rawResp, nil
		}
		return nil, err
	}

	return
}

func (wd *Driver) tempHttpGET(pathElem ...string) (rawResp rawResponse, err error) {
	return wd.tempHttpRequest(http.MethodGet, wd.concatURL(nil, pathElem...), nil)
}

func (wd *Driver) tempHttpPOST(data interface{}, pathElem ...string) (rawResp rawResponse, err error) {
	var bsJSON []byte = nil
	if data != nil {
		if bsJSON, err = json.Marshal(data); err != nil {
			return nil, err
		}
	}
	return wd.tempHttpRequest(http.MethodPost, wd.concatURL(nil, pathElem...), bsJSON)
}

func (wd *Driver) tempHttpDELETE(pathElem ...string) (rawResp rawResponse, err error) {
	return wd.tempHttpRequest(http.MethodDelete, wd.concatURL(nil, pathElem...), nil)
}

func (wd *Driver) tempHttpRequest(method string, rawURL string, rawBody []byte) (rawResp rawResponse, err error) {
	var localPort int
	{
		tmpURL, _ := url.Parse(rawURL)
		hostname := tmpURL.Hostname()
		if strings.HasPrefix(hostname, forwardToPrefix) {
			localPort, _ = strconv.Atoi(strings.TrimPrefix(hostname, forwardToPrefix))
			rawURL = strings.Replace(rawURL, hostname, "localhost", 1)
		}
	}

	var req *http.Request

	tmpHTTPClient := HTTPClient

	var resp *http.Response
	retryCount := 5
	for retryCount > 0 {
		log.Info().Str("url", rawURL).Msg("request url")
		if req, err = http.NewRequest(method, rawURL, bytes.NewBuffer(rawBody)); err != nil {
			return
		}
		for k, v := range uia2Header {
			req.Header.Set(k, v)
		}

		if localPort != 0 {
			var conn net.Conn
			if conn, err = net.Dial("tcp", fmt.Sprintf(":%d", localPort)); err != nil {
				return nil, fmt.Errorf("adb forward: %w", err)
			}
			tmpHTTPClient.Transport = &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return conn, nil
				},
			}
			defer func() { _ = conn.Close() }()
		}

		log.Info().Str("url", rawURL).Msg("do request")
		resp, err = tmpHTTPClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if err != nil {
			log.Error().Str("err", err.Error()).Msg("get response")
		}

		time.Sleep(3 * time.Second)
		retryCount -= 1

		log.Info().Msg("get new session id")
		sessionID, err2 := wd.getSessionID()
		if err2 != nil {
			log.Error().Str("err", err2.Error()).Msg("get new session id")
			continue
		}

		oriSessionId := wd.sessionId
		wd.sessionId = sessionID
		if oriSessionId != "" {
			rawURL = strings.Replace(rawURL, oriSessionId, wd.sessionId, 1)
		}
		log.Info().Str("oldSessionId", oriSessionId).Str("newSessionId", wd.sessionId).Msg("replace sessionId successful")
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	rawResp, err = ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	var reply = new(struct {
		Value struct {
			Err        string `json:"error"`
			Message    string `json:"message"`
			Stacktrace string `json:"stacktrace"`
		}
	})
	if err = json.Unmarshal(rawResp, reply); err != nil {
		if resp.StatusCode == http.StatusOK {
			// 如果遇到 value 直接是 字符串，则报错，但是 http 状态是 200
			// {"sessionId":"b4f2745a-be74-4cb3-8f4c-881cde817a8d","value":"YWJjZDEyMw==\n"}
			return rawResp, nil
		}
		return nil, err
	}
	if reply.Value.Err != "" {
		return nil, fmt.Errorf("%s: %s", reply.Value.Err, reply.Value.Message)
	}

	return
}

func (wd *Driver) getSessionID() (sessionID string, err error) {
	var localPort int
	var bsJSON []byte = nil
	var rawResp rawResponse
	rawURL := wd.concatURL(nil, "/session")
	{
		tmpURL, _ := url.Parse(wd.concatURL(nil, rawURL))
		hostname := tmpURL.Hostname()
		if strings.HasPrefix(hostname, forwardToPrefix) {
			localPort, _ = strconv.Atoi(strings.TrimPrefix(hostname, forwardToPrefix))
			rawURL = strings.Replace(rawURL, hostname, "localhost", 1)
		}
	}

	tmpHTTPClient := HTTPClient

	var resp *http.Response

	var err2 error
	capabilities := NewCapabilities()
	data := map[string]interface{}{"capabilities": capabilities}
	if data != nil {
		if bsJSON, err2 = json.Marshal(data); err2 != nil {
			return "", err2
		}
	}

	log.Info().Str("url", rawURL).Msg("request url")
	var req *http.Request
	if req, err = http.NewRequest("POST", rawURL, bytes.NewBuffer(bsJSON)); err != nil {
		return
	}
	for k, v := range uia2Header {
		req.Header.Set(k, v)
	}

	if localPort != 0 {
		var conn net.Conn
		if conn, err = net.Dial("tcp", fmt.Sprintf(":%d", localPort)); err != nil {
			return "", fmt.Errorf("adb forward: %w", err)
		}
		tmpHTTPClient.Transport = &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			},
		}
		defer func() { _ = conn.Close() }()
	}

	resp, err = tmpHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	rawResp, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var reply = new(struct {
		Value struct {
			Err        string `json:"error"`
			Message    string `json:"message"`
			Stacktrace string `json:"stacktrace"`
			SessionId  string
		}
	})
	if err = json.Unmarshal(rawResp, reply); err != nil {
		if resp.StatusCode != http.StatusOK {
			return "", err
		}
		return "", err
	}
	if reply.Value.Err != "" {
		return "", fmt.Errorf("%s: %s", reply.Value.Err, reply.Value.Message)
	}
	// 如果遇到 value 直接是 字符串，则报错，但是 http 状态是 200
	// {"sessionId":"b4f2745a-be74-4cb3-8f4c-881cde817a8d","value":"YWJjZDEyMw==\n"}
	if err2 = json.Unmarshal(rawResp, reply); err2 != nil {
		return "", errors.New(fmt.Sprintf("%s%s", err.Error(), err2.Error()))
	}
	return reply.Value.SessionId, nil
}

func convertToHTTPClient(conn net.Conn) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			},
		},
		Timeout: 0,
	}
}
