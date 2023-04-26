package uixt

import (
	"bytes"
	"fmt"
	"image"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/internal/json"
)

var client = &http.Client{
	Timeout: time.Second * 10,
}

type OCRResult struct {
	Text   string   `json:"text"`
	Points []PointF `json:"points"`
}

type ResponseOCR struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	OCRResult []OCRResult `json:"ocrResult"`
}

type veDEMOCRService struct{}

func newVEDEMOCRService() (*veDEMOCRService, error) {
	if err := checkEnv(); err != nil {
		return nil, err
	}
	return &veDEMOCRService{}, nil
}

func checkEnv() error {
	if env.VEDEM_OCR_URL == "" {
		return errors.Wrap(code.OCREnvMissedError, "VEDEM_OCR_URL missed")
	}
	if env.VEDEM_OCR_AK == "" {
		return errors.Wrap(code.OCREnvMissedError, "VEDEM_OCR_AK missed")
	}
	if env.VEDEM_OCR_SK == "" {
		return errors.Wrap(code.OCREnvMissedError, "VEDEM_OCR_SK missed")
	}
	return nil
}

func (s *veDEMOCRService) getOCRResult(imageBuf *bytes.Buffer) ([]OCRResult, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)
	bodyWriter.WriteField("withDet", "true")
	// bodyWriter.WriteField("timestampOnly", "true")

	formWriter, err := bodyWriter.CreateFormFile("image", "screenshot.png")
	if err != nil {
		return nil, errors.Wrap(code.OCRRequestError,
			fmt.Sprintf("create form file error: %v", err))
	}
	size, err := formWriter.Write(imageBuf.Bytes())
	if err != nil {
		return nil, errors.Wrap(code.OCRRequestError,
			fmt.Sprintf("write form error: %v", err))
	}

	err = bodyWriter.Close()
	if err != nil {
		return nil, errors.Wrap(code.OCRRequestError,
			fmt.Sprintf("close body writer error: %v", err))
	}

	req, err := http.NewRequest("POST", env.VEDEM_OCR_URL, bodyBuf)
	if err != nil {
		return nil, errors.Wrap(code.OCRRequestError,
			fmt.Sprintf("construct request error: %v", err))
	}

	token := builtin.Sign("auth-v2", env.VEDEM_OCR_AK, env.VEDEM_OCR_SK, bodyBuf.Bytes())
	req.Header.Add("Agw-Auth", token)
	req.Header.Add("Content-Type", bodyWriter.FormDataContentType())

	var resp *http.Response
	// retry 3 times
	for i := 1; i <= 3; i++ {
		resp, err = client.Do(req)
		var logID string
		if resp != nil {
			logID = getLogID(resp.Header)
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Debug().
				Str("X-TT-LOGID", logID).
				Int("imageBufSize", size).
				Msg("request OCR service success")
			break
		}
		log.Error().Err(err).
			Str("X-TT-LOGID", logID).
			Int("imageBufSize", size).
			Msgf("request OCR service failed, retry %d", i)
		time.Sleep(1 * time.Second)
	}
	if resp == nil {
		return nil, code.OCRServiceConnectionError
	}

	defer resp.Body.Close()

	results, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(code.OCRResponseError,
			fmt.Sprintf("read response body error: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrap(code.OCRResponseError,
			fmt.Sprintf("unexpected response status code: %d, results: %v",
				resp.StatusCode, string(results)))
	}

	var ocrResult ResponseOCR
	err = json.Unmarshal(results, &ocrResult)
	if err != nil {
		return nil, errors.Wrap(code.OCRResponseError,
			fmt.Sprintf("json unmarshal response body error: %v", err))
	}

	return ocrResult.OCRResult, nil
}

func getLogID(header http.Header) string {
	if len(header) == 0 {
		return ""
	}

	logID, ok := header["X-Tt-Logid"]
	if !ok || len(logID) == 0 {
		return ""
	}
	return logID[0]
}

type OCRText struct {
	Text string
	Rect image.Rectangle
}

type OCRTexts []OCRText

func (t OCRTexts) Texts() (texts []string) {
	for _, text := range t {
		texts = append(texts, text.Text)
	}
	return texts
}

func (s *veDEMOCRService) GetTexts(imageBuf *bytes.Buffer, options ...DataOption) (
	ocrTexts OCRTexts, err error) {

	ocrResults, err := s.getOCRResult(imageBuf)
	if err != nil {
		log.Error().Err(err).Msg("getOCRResult failed")
		return
	}

	dataOptions := NewDataOptions(options...)

	for _, ocrResult := range ocrResults {
		rect := image.Rectangle{
			// ocrResult.Points 顺序：左上 -> 右上 -> 右下 -> 左下
			Min: image.Point{
				X: int(ocrResult.Points[0].X),
				Y: int(ocrResult.Points[0].Y),
			},
			Max: image.Point{
				X: int(ocrResult.Points[2].X),
				Y: int(ocrResult.Points[2].Y),
			},
		}

		// check if text in scope
		if rect.Min.X < dataOptions.Scope[0] || rect.Max.X > dataOptions.Scope[2] ||
			rect.Min.Y < dataOptions.Scope[1] || rect.Max.Y > dataOptions.Scope[3] {
			// not in scope
			continue
		}

		ocrTexts = append(ocrTexts, OCRText{
			Text: ocrResult.Text,
			Rect: rect,
		})
	}
	return
}

func (s *veDEMOCRService) FindText(text string, imageBuf *bytes.Buffer, options ...DataOption) (
	rect image.Rectangle, err error) {

	ocrTexts, err := s.GetTexts(imageBuf, options...)
	if err != nil {
		log.Error().Err(err).Msg("GetTexts failed")
		return
	}

	dataOptions := NewDataOptions(options...)

	var rects []image.Rectangle
	for _, ocrText := range ocrTexts {
		rect = ocrText.Rect

		// not contains text
		if !strings.Contains(ocrText.Text, text) {
			continue
		}

		rects = append(rects, rect)

		// contains text while not match exactly
		if ocrText.Text != text {
			continue
		}

		// match exactly, and not specify index, return the first one
		if dataOptions.Index == 0 {
			return rect, nil
		}
	}

	if len(rects) == 0 {
		return image.Rectangle{}, errors.Wrap(code.OCRTextNotFoundError,
			fmt.Sprintf("text %s not found in %v", text, ocrTexts.Texts()))
	}

	// get index
	idx := dataOptions.Index
	if idx > 0 {
		// NOTICE: index start from 1
		idx = idx - 1
	} else if idx < 0 {
		idx = len(rects) + idx
	}

	// index out of range
	if idx >= len(rects) {
		return image.Rectangle{}, errors.Wrap(code.OCRTextNotFoundError,
			fmt.Sprintf("text %s found %d, index %d out of range", text, len(rects), idx))
	}

	return rects[idx], nil
}

func (s *veDEMOCRService) FindTexts(texts []string, imageBuf *bytes.Buffer, options ...DataOption) (
	rects []image.Rectangle, err error) {

	ocrTexts, err := s.GetTexts(imageBuf, options...)
	if err != nil {
		log.Error().Err(err).Msg("GetTexts failed")
		return
	}

	var success bool
	for _, text := range texts {
		var found bool
		for _, ocrText := range ocrTexts {
			rect := ocrText.Rect

			// not contains text
			if !strings.Contains(ocrText.Text, text) {
				continue
			}

			found = true
			rects = append(rects, rect)
			break
		}
		if !found {
			rects = append(rects, image.Rectangle{})
		}
		success = found || success
	}

	if !success {
		return rects, errors.Wrap(code.OCRTextNotFoundError,
			fmt.Sprintf("texts %s not found in %v", texts, ocrTexts.Texts()))
	}

	return rects, nil
}
