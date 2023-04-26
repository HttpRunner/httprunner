package uixt

import (
	"bytes"
	"fmt"
	"image"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/internal/json"
)

type CPResult struct {
	Point  PointF  `json:"point"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type CPResponse struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Result  []CPResult `json:"result"`
}

type veDEMCPService struct{}

func newVEDEMCPService() (*veDEMCPService, error) {
	if err := checkCPEnv(); err != nil {
		return nil, err
	}
	return &veDEMCPService{}, nil
}

func checkCPEnv() error {
	if env.VEDEM_CP_URL == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_CP_URL missed")
	}
	if env.VEDEM_CP_AK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_CP_AK missed")
	}
	if env.VEDEM_CP_SK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_CP_SK missed")
	}
	return nil
}

func (s *veDEMCPService) getCPResult(sourceImage []byte) ([]CPResult, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)
	bodyWriter.WriteField("withDet", "true")
	// bodyWriter.WriteField("timestampOnly", "true")

	formWriter, err := bodyWriter.CreateFormFile("image", "image.png")
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("create form file error: %v", err))
	}
	_, err = formWriter.Write(sourceImage)
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("write form error: %v", err))
	}

	err = bodyWriter.Close()
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("close body writer error: %v", err))
	}

	req, err := http.NewRequest("POST", env.VEDEM_CP_URL, bodyBuf)
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("construct request error: %v", err))
	}

	token := builtin.Sign("auth-v2", env.VEDEM_CP_AK, env.VEDEM_CP_SK, bodyBuf.Bytes())
	req.Header.Add("Agw-Auth", token)
	req.Header.Add("Content-Type", bodyWriter.FormDataContentType())

	var resp *http.Response
	// retry 3 times
	for i := 1; i <= 3; i++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}

		var logID string
		if resp != nil {
			logID = getLogID(resp.Header)
		}
		log.Error().Err(err).
			Str("logID", logID).
			Msgf("request CV service failed, retry %d", i)
		time.Sleep(1 * time.Second)
	}
	if resp == nil {
		return nil, code.CVServiceConnectionError
	}

	defer resp.Body.Close()

	results, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("read response body error: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("unexpected response status code: %d, results: %v",
				resp.StatusCode, string(results)))
	}

	var cpResult CPResponse
	err = json.Unmarshal(results, &cpResult)
	if err != nil {
		return nil, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("json unmarshal response body error: %v", err))
	}

	return cpResult.Result, nil
}

func (s *veDEMCPService) FindPopupCloseButton(byteSource []byte, options ...DataOption) (rect image.Rectangle, err error) {
	data := NewDataOptions(options...)

	cpResults, err := s.getCPResult(byteSource)
	if err != nil {
		log.Error().Err(err).Msg("getCPResult failed")
		return
	}

	var rects []image.Rectangle
	for _, cpResult := range cpResults {
		rect = image.Rectangle{
			// cvResult.Points 顺序：左上 -> 右上 -> 右下 -> 左下
			Min: image.Point{
				X: int(cpResult.Point.X),
				Y: int(cpResult.Point.Y),
			},
			Max: image.Point{
				X: int(cpResult.Point.X + cpResult.Width),
				Y: int(cpResult.Point.Y + cpResult.Height),
			},
		}
		if rect.Min.X >= data.Scope[0] && rect.Max.X <= data.Scope[2] && rect.Min.Y >= data.Scope[1] && rect.Max.Y <= data.Scope[3] {
			rects = append(rects, rect)

			// match exactly, and not specify index, return the first one
			if data.Index == 0 {
				return rect, nil
			}
		}
	}

	if len(rects) == 0 {
		return image.Rectangle{}, errors.Wrap(code.CVImageNotFoundError,
			fmt.Sprintf("popup close button not found"))
	}

	// get index
	idx := data.Index
	if idx > 0 {
		// NOTICE: index start from 1
		idx = idx - 1
	} else if idx < 0 {
		idx = len(rects) + idx
	}

	// index out of range
	if idx >= len(rects) {
		return image.Rectangle{}, errors.Wrap(code.CVImageNotFoundError,
			fmt.Sprintf("popup close button found, index %d out of range", idx))
	}

	return rects[idx], nil
}
