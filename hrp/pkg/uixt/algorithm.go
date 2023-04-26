package uixt

import (
	"bytes"
	"fmt"
	"image"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	IconMatching MatchMethod = iota
	TemplateMatching
	MultiScaleTemplateMatchingPre
	MultiScaleTemplateMatching
	KAZEMatching
	BRISKMatching
	AKAZEMatching
	ORBMatching
	SIFTMatching
	SURFMatching
	BRIEFMatching
)

func getBufFromDisk(name string) (*bytes.Buffer, error) {
	var f *os.File
	var err error
	if f, err = os.Open(name); err != nil {
		return nil, err
	}
	var all []byte
	if all, err = ioutil.ReadAll(f); err != nil {
		return nil, err
	}
	return bytes.NewBuffer(all), nil
}

func getBufFromNetwork(name string) (*bytes.Buffer, error) {
	res, err := http.Get(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = res.Body.Close()
	}()

	imageBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return bytes.NewBuffer(imageBytes), nil
}

type ImageTemplate struct {
	FilePath  string `json:"filepath"`
	rawBuffer *bytes.Buffer
}

func NewImageTemplate(filepath string) ImageTemplate {
	return ImageTemplate{
		FilePath:  filepath,
		rawBuffer: bytes.NewBuffer([]byte{}),
	}
}

func (i *ImageTemplate) read() (imageBuffer *bytes.Buffer, err error) {
	if i.rawBuffer.Len() != 0 {
		return i.rawBuffer, nil
	}
	if strings.HasPrefix(i.FilePath, "http://") || strings.HasPrefix(i.FilePath, "https://") {
		imageBuffer, err = getBufFromNetwork(i.FilePath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to request image")
		}
	} else if builtin.IsFilePathExists(i.FilePath) {
		imageBuffer, err = getBufFromDisk(i.FilePath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read image")
		}
	} else {
		return nil, errors.New(fmt.Sprintf("not found image: %s", i.FilePath))
	}
	i.rawBuffer = imageBuffer

	return imageBuffer, nil
}

// extendCV 获得扩展后的 Driver，
// 并指定匹配阀值，
// 获取当前设备的 Scale，
func (dExt *DriverExt) extendCV(options ...CVOption) (err error) {
	for _, option := range options {
		option(&dExt.CVArgs)
	}

	if dExt.threshold == 0 {
		dExt.threshold = 0.95 // default threshold
	}

	return
}

func (dExt *DriverExt) OnlyOnceThreshold(threshold float64) (newExt *DriverExt) {
	newExt = new(DriverExt)
	newExt.Driver = dExt.Driver
	newExt.scale = dExt.scale
	newExt.matchMode = dExt.matchMode
	newExt.threshold = threshold
	return
}

func (dExt *DriverExt) OnlyOnceMatchMode(matchMode MatchMode) (newExt *DriverExt) {
	newExt = new(DriverExt)
	newExt.Driver = dExt.Driver
	newExt.scale = dExt.scale
	newExt.matchMode = matchMode
	newExt.threshold = dExt.threshold
	return
}

// func (sExt *DriverExt) findImgRect(search string) (rect image.Rectangle, err error) {
// 	pathSource := filepath.Join(sExt.pathname, GenFilename())
// 	if err = sExt.driver.ScreenshotToDisk(pathSource); err != nil {
// 		return image.Rectangle{}, err
// 	}
//
// 	if rect, err = FindImageRectFromDisk(pathSource, search, float32(sExt.Threshold), MatchMode(sExt.MatchMode)); err != nil {
// 		return image.Rectangle{}, err
// 	}
// 	return
// }

func (dExt *DriverExt) FindAllImageRect(search string, options ...DataOption) (rects []image.Rectangle, err error) {
	var bufSource, bufSearch *bytes.Buffer
	im := NewImageTemplate(search)
	if bufSearch, err = im.read(); err != nil {
		return nil, err
	}
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		return nil, err
	}

	switch dExt.matchMethod {
	case IconMatching:
		fallthrough
	case TemplateMatching:
		rects, err = FindAllImageRectsFromRaw(bufSource, bufSearch, float32(dExt.threshold), MatchMode(dExt.matchMode))
	}
	return rects, err
}

func (dExt *DriverExt) FindImageRectInUIKit(imagePath string, options ...DataOption) (x, y, width, height float64, err error) {
	var bufSource, bufSearch *bytes.Buffer
	im := NewImageTemplate(imagePath)
	if bufSearch, err = im.read(); err != nil {
		return 0, 0, 0, 0, err
	}
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		return 0, 0, 0, 0, err
	}

	var rect image.Rectangle
	switch dExt.matchMethod {
	case IconMatching:
		service, err := newVEDEMIMService()
		if err != nil {
			return 0, 0, 0, 0, err
		}
		rect, err = service.FindImage(bufSearch.Bytes(), bufSource.Bytes(), options...)
	case TemplateMatching:
		rect, err = FindImageRectFromRaw(bufSource, bufSearch, float32(dExt.threshold), MatchMode(dExt.matchMode))
	default:
		return 0, 0, 0, 0, errors.New("method not supported")
	}

	if err != nil {
		return 0, 0, 0, 0, err
	}

	// if rect, err = dExt.findImgRect(search); err != nil {
	// 	return 0, 0, 0, 0, err
	// }
	x, y, width, height = dExt.MappingToRectInUIKit(rect)
	return
}

type CVService interface {
	FindImage(byteSearch []byte, byteSource []byte, options ...DataOption) (rect image.Rectangle, err error)
}

func (dExt *DriverExt) FindImageByCV(cvImage string, options ...DataOption) (rect image.Rectangle, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	service, err := newVEDEMIMService()
	if err != nil {
		return
	}
	im := NewImageTemplate(cvImage)
	bufSearch, err := im.read()
	if err != nil {
		log.Error().Err(err).Msg("failed to get image")
		return
	}
	rect, err = service.FindImage(bufSearch.Bytes(), bufSource.Bytes(), options...)
	if err != nil {
		log.Warn().Msgf("FindImage failed: %s", err.Error())
		return
	}

	log.Info().Str("cvImage", cvImage).
		Interface("rect", rect).Msgf("FindImageByCV success")
	return
}

func (dExt *DriverExt) ClosePopupHandler() {
	retryCount := 3 // 重试次数
	for retryCount > 0 {
		rect, err := dExt.FindPopupCloseButton()
		if err != nil {
			break
		}

		x, y, width, height := dExt.MappingToRectInUIKit(rect)
		pointX := x + width*0.5
		pointY := y + height*0.5
		err = dExt.Driver.TapFloat(pointX, pointY)
		if err != nil {
			break
		}

		retryCount--
	}
}

func (dExt *DriverExt) FindPopupCloseButton(options ...DataOption) (rect image.Rectangle, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	service, err := newVEDEMCPService()
	if err != nil {
		return
	}

	rect, err = service.FindPopupCloseButton(bufSource.Bytes(), options...)
	if err != nil {
		log.Warn().Msgf("FindPopupCloseButton failed: %s", err.Error())
		return
	}

	log.Info().Interface("rect", rect).Msgf("FindPopupCloseButton success")
	return
}

type OCRService interface {
	GetTexts(imageBuf *bytes.Buffer, options ...DataOption) (ocrTexts OCRTexts, err error)
	FindText(text string, imageBuf *bytes.Buffer, options ...DataOption) (rect image.Rectangle, err error)
	FindTexts(texts []string, imageBuf *bytes.Buffer, options ...DataOption) (rects []image.Rectangle, err error)
}

func (dExt *DriverExt) GetTextsByOCR(options ...DataOption) (texts OCRTexts, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	ocrTexts, err := dExt.ocrService.GetTexts(bufSource, options...)
	if err != nil {
		log.Error().Err(err).Msg("GetTexts failed")
		return
	}

	return ocrTexts, nil
}

func (dExt *DriverExt) FindTextByOCR(ocrText string, options ...DataOption) (x, y, width, height float64, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	rect, err := dExt.ocrService.FindText(ocrText, bufSource, options...)
	if err != nil {
		log.Warn().Msgf("FindText failed: %s", err.Error())
		return
	}

	log.Info().Str("ocrText", ocrText).
		Interface("rect", rect).Msgf("FindTextByOCR success")
	x, y, width, height = dExt.MappingToRectInUIKit(rect)
	return
}

func (dExt *DriverExt) FindTextsByOCR(ocrTexts []string, options ...DataOption) (points [][]float64, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	rects, err := dExt.ocrService.FindTexts(ocrTexts, bufSource, options...)
	if err != nil {
		log.Warn().Msgf("FindTexts failed: %s", err.Error())
		return
	}

	log.Info().Interface("ocrTexts", ocrTexts).
		Interface("rects", rects).Msgf("FindTextsByOCR success")
	for _, rect := range rects {
		x, y, width, height := dExt.MappingToRectInUIKit(rect)
		points = append(points, []float64{x, y, width, height})
	}

	return
}

type SDService interface {
	SceneDetection(detectImage []byte, detectType string) (bool, error)
}

func (dExt *DriverExt) ScenarioDetect(scenarioType string, options ...DataOption) (res bool, err error) {
	var bufSource *bytes.Buffer
	if bufSource, err = dExt.TakeScreenShot(); err != nil {
		err = fmt.Errorf("TakeScreenShot error: %v", err)
		return
	}

	service, err := newVEDEMSDService()
	if err != nil {
		return
	}
	return service.SceneDetection(bufSource.Bytes(), scenarioType)
}
