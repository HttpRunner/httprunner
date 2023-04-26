package uixt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/internal/json"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Box struct {
	Point  PointF  `json:"point"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type IMResult struct {
	Box      Box     `json:"box"`
	Distance float64 `json:"distance"`
}

type IMResponse struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Result  []IMResult `json:"result"`
}

type veDEMIMService struct{}

func newVEDEMIMService() (*veDEMIMService, error) {
	if err := checkIMEnv(); err != nil {
		return nil, err
	}
	return &veDEMIMService{}, nil
}

func checkIMEnv() error {
	if env.VEDEM_IM_URL == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_IM_URL missed")
	}
	if env.VEDEM_IM_AK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_IM_AK missed")
	}
	if env.VEDEM_IM_SK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_IM_SK missed")
	}
	return nil
}

func convertBase64(imgByte []byte) (baseImg string) {
	return base64.RawStdEncoding.EncodeToString(imgByte)
}

func (s *veDEMIMService) getIMResult(searchImage []byte, sourceImage []byte) ([]IMResult, error) {
	data := map[string]interface{}{
		"sourceImage":  convertBase64(sourceImage),
		"targetImages": []string{convertBase64(searchImage)},
	}

	// post json
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("json marshal error: %v", err))
	}

	req, err := http.NewRequest("POST", env.VEDEM_IM_URL, io.NopCloser(bytes.NewReader(dataBytes)))
	if err != nil {
		return nil, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("construct request error: %v", err))
	}

	token := builtin.Sign("auth-v2", env.VEDEM_IM_AK, env.VEDEM_IM_SK, dataBytes)
	req.Header.Add("Agw-Auth", token)
	req.Header.Add("Content-Type", "application/json; charset=utf-8")

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

	var cvResult IMResponse
	err = json.Unmarshal(results, &cvResult)
	if err != nil {
		return nil, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("json unmarshal response body error: %v", err))
	}

	return cvResult.Result, nil
}

func (s *veDEMIMService) FindImage(byteSearch []byte, byteSource []byte, options ...DataOption) (rect image.Rectangle, err error) {
	data := NewDataOptions(options...)

	cvResults, err := s.getIMResult(byteSearch, byteSource)
	if err != nil {
		log.Error().Err(err).Msg("getIMResult failed")
		return
	}

	var rects []image.Rectangle
	for _, cvResult := range cvResults {
		rect = image.Rectangle{
			// cvResult.Points 顺序：左上 -> 右上 -> 右下 -> 左下
			Min: image.Point{
				X: int(cvResult.Box.Point.X),
				Y: int(cvResult.Box.Point.Y),
			},
			Max: image.Point{
				X: int(cvResult.Box.Point.X + cvResult.Box.Width),
				Y: int(cvResult.Box.Point.Y + cvResult.Box.Height),
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
			fmt.Sprintf("image not found"))
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
			fmt.Sprintf("image found, index %d out of range", idx))
	}

	return rects[idx], nil
}
