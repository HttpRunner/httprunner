package uixt

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/internal/json"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	CHECK_LIVE_TYPE_GAME      = "checkLiveTypeGame"     // 直播游戏
	CHECK_LIVE_TYPE_SHOW      = "checkLiveTypeShow"     // 直播秀场
	CHECK_LIVE_TYPE_PEOPLE    = "checkLiveTypePeople"   // 直播多人
	CHECK_LIVE_TYPE_SHOP      = "checkLiveTypeShop"     // 直播电商
	CHECK_BLACK_OR_WHITE      = "checkBlackOrWhite"     // 黑白屏
	CHECK_PART_BLACK_OR_WHITE = "checkPartBlackOrWhite" // 部分黑白屏
	CHECK_HALF_BLANK          = "checkHalfBlank"        // 半白屏检测
	CHECK_PAGE_ERROR          = "checkPageError"        // 页面乱码
	CHECK_SNOW                = "checkSnow"             // 雪花屏
	CHECK_OVER_LAY            = "checkOverlay"          // 图像重叠
	CHECK_LOAD_FAILED         = "checkLoadFailed"       // 图像加载失败检测
	CHECK_DETECT_COLOR_BLOCK  = "detectColorBlock"      // 游戏色块检测
	CHECK_PURPLE              = "checkPurple"           // 游戏紫块
	CHECK_WHITE_RECT          = "checkWhiteRect"        // 游戏白块
	CHECK_CORRUPT             = "checkCorrupt"          // 游戏花屏
	CHECK_BLACK_EDGE          = "checkBlackEdge"        // 游戏黑边
	CHECK_WHITE_RATIO         = "checkWhiteRatio"       // 白屏占比
	CHECK_OVER_EXPOSURE       = "checkOverExposure"     // 图像过爆
	CHECK_DIALOG              = "checkDialog"           // 弹窗检测
	CHECK_TEXT_OVER_LAP       = "checkTextOverlap"      // 文字重叠
	CHECK_TEXT_OVER_STEP      = "checkTextOverstep"     // 文字超框
	CHECK_VIDEO_CORRUPT       = "checkVideoCorrupt"     // 视频花屏
	CHECK_GREEN_VIDEO         = "checkGreenVideo"       // 视频绿屏
	CHECK_DEFAULTCHECKED      = "checkDefaultChecked"   // 合规检测
)

type SDResult struct {
	Image  string   `json:"image"`
	Points []PointF `json:"points"`
}

type SDResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  bool   `json:"result"`
}

type veDEMSDService struct{}

func newVEDEMSDService() (*veDEMSDService, error) {
	if err := checkSDEnv(); err != nil {
		return nil, err
	}
	return &veDEMSDService{}, nil
}

func checkSDEnv() error {
	if env.VEDEM_SD_URL == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_SD_URL missed")
	}
	if env.VEDEM_SD_AK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_SD_AK missed")
	}
	if env.VEDEM_SD_SK == "" {
		return errors.Wrap(code.CVEnvMissedError, "VEDEM_SD_SK missed")
	}
	return nil
}

func (s *veDEMSDService) SceneDetection(detectImage []byte, detectType string) (bool, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)
	bodyWriter.WriteField("withDet", "true")
	bodyWriter.WriteField("detectType", detectType)
	// bodyWriter.WriteField("timestampOnly", "true")

	formWriter, err := bodyWriter.CreateFormFile("image", "image.png")
	if err != nil {
		return false, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("create form file error: %v", err))
	}
	size, err := formWriter.Write(detectImage)
	if err != nil {
		return false, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("write form error: %v", err))
	}

	err = bodyWriter.Close()
	if err != nil {
		return false, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("close body writer error: %v", err))
	}

	req, err := http.NewRequest("POST", env.VEDEM_SD_URL, bodyBuf)
	if err != nil {
		return false, errors.Wrap(code.CVRequestError,
			fmt.Sprintf("construct request error: %v", err))
	}

	token := builtin.Sign("auth-v2", env.VEDEM_SD_AK, env.VEDEM_SD_SK, bodyBuf.Bytes())
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
			Int("imageBufSize", size).
			Msgf("request CV service failed, retry %d", i)
		time.Sleep(1 * time.Second)
	}
	if resp == nil {
		return false, code.CVServiceConnectionError
	}

	defer resp.Body.Close()

	results, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("read response body error: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return false, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("unexpected response status code: %d, results: %v",
				resp.StatusCode, string(results)))
	}

	var sdResult SDResponse
	err = json.Unmarshal(results, &sdResult)
	if err != nil {
		return false, errors.Wrap(code.CVResponseError,
			fmt.Sprintf("json unmarshal response body error: %v", err))
	}

	return sdResult.Result, nil
}
