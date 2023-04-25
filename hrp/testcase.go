package hrp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
)

// ITestCase represents interface for testcases,
// includes TestCase and TestCasePath.
type ITestCase interface {
	GetPath() string
	ToTestCase() (*TestCase, error)
}

// TestCase is a container for one testcase, which is used for testcase runner.
// TestCase implements ITestCase interface.
type TestCase struct {
	Config    *TConfig
	TestSteps []IStep
}

func (tc *TestCase) GetPath() string {
	return tc.Config.Path
}

func (tc *TestCase) ToTestCase() (*TestCase, error) {
	return tc, nil
}

func (tc *TestCase) ToTCase() *TCase {
	tCase := &TCase{
		Config: tc.Config,
	}
	for _, step := range tc.TestSteps {
		if step.Type() == stepTypeTestCase {
			if testcase, ok := step.Struct().TestCase.(*TestCase); ok {
				step.Struct().TestCase = testcase.ToTCase()
			}
		}
		tCase.TestSteps = append(tCase.TestSteps, step.Struct())
	}
	return tCase
}

func (tc *TestCase) Dump2JSON(targetPath string) error {
	tCase := tc.ToTCase()
	err := builtin.Dump2JSON(tCase, targetPath)
	if err != nil {
		return errors.Wrap(err, "dump testcase to json failed")
	}
	return nil
}

func (tc *TestCase) Dump2YAML(targetPath string) error {
	tCase := tc.ToTCase()
	err := builtin.Dump2YAML(tCase, targetPath)
	if err != nil {
		return errors.Wrap(err, "dump testcase to yaml failed")
	}
	return nil
}

// TestCasePath implements ITestCase interface.
type TestCasePath string

func (path *TestCasePath) GetPath() string {
	return fmt.Sprintf("%v", *path)
}

// ToTestCase loads testcase path and convert to *TestCase
func (path *TestCasePath) ToTestCase() (*TestCase, error) {
	tc := &TCase{}
	casePath := path.GetPath()
	err := builtin.LoadFile(casePath, tc)
	if err != nil {
		return nil, err
	}
	return tc.ToTestCase(casePath)
}

// TCase represents testcase data structure.
// Each testcase includes one public config and several sequential teststeps.
type TCase struct {
	Config    *TConfig `json:"config" yaml:"config"`
	TestSteps []*TStep `json:"teststeps" yaml:"teststeps"`
}

// MakeCompat converts TCase compatible with Golang engine style
func (tc *TCase) MakeCompat() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("[MakeCompat] convert compat testcase error: %v", p)
		}
	}()
	for _, step := range tc.TestSteps {
		// 1. deal with request body compatibility
		convertCompatRequestBody(step.Request)

		// 2. deal with validators compatibility
		err = convertCompatValidator(step.Validators)
		if err != nil {
			return err
		}

		// 3. deal with extract expr including hyphen
		convertExtract(step.Extract)
	}
	return nil
}

func (tc *TCase) ToTestCase(casePath string) (*TestCase, error) {
	if tc.TestSteps == nil {
		return nil, errors.Wrap(code.InvalidCaseFormat,
			"invalid testcase format, missing teststeps!")
	}

	if tc.Config == nil {
		tc.Config = &TConfig{Name: "please input testcase name"}
	}
	tc.Config.Path = casePath
	return tc.toTestCase()
}

// toTestCase converts *TCase to *TestCase
func (tc *TCase) toTestCase() (*TestCase, error) {
	testCase := &TestCase{
		Config: tc.Config,
	}

	err := tc.MakeCompat()
	if err != nil {
		return nil, err
	}

	// locate project root dir by plugin path
	projectRootDir, err := GetProjectRootDirPath(tc.Config.Path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get project root dir")
	}

	// load .env file
	dotEnvPath := filepath.Join(projectRootDir, ".env")
	if builtin.IsFilePathExists(dotEnvPath) {
		envVars := make(map[string]string)
		err = builtin.LoadFile(dotEnvPath, envVars)
		if err != nil {
			return nil, errors.Wrap(err, "failed to load .env file")
		}

		// override testcase config env with variables loaded from .env file
		// priority: .env file > testcase config env
		if testCase.Config.Environs == nil {
			testCase.Config.Environs = make(map[string]string)
		}
		for key, value := range envVars {
			testCase.Config.Environs[key] = value
		}
	}

	for _, step := range tc.TestSteps {
		if step.API != nil {
			apiPath, ok := step.API.(string)
			if ok {
				path := filepath.Join(projectRootDir, apiPath)
				if !builtin.IsFilePathExists(path) {
					return nil, errors.Wrap(code.ReferencedFileNotFound,
						fmt.Sprintf("referenced api file not found: %s", path))
				}

				refAPI := APIPath(path)
				apiContent, err := refAPI.ToAPI()
				if err != nil {
					return nil, err
				}
				step.API = apiContent
			} else {
				apiMap, ok := step.API.(map[string]interface{})
				if !ok {
					return nil, errors.Wrap(code.InvalidCaseFormat,
						fmt.Sprintf("referenced api should be map or path(string), got %v", step.API))
				}
				api := &API{}
				err = mapstructure.Decode(apiMap, api)
				if err != nil {
					return nil, err
				}
				step.API = api
			}
			_, ok = step.API.(*API)
			if !ok {
				return nil, errors.Wrap(code.InvalidCaseFormat,
					fmt.Sprintf("failed to handle referenced API, got %v", step.TestCase))
			}
			testCase.TestSteps = append(testCase.TestSteps, &StepAPIWithOptionalArgs{
				step: step,
			})
		} else if step.TestCase != nil {
			casePath, ok := step.TestCase.(string)
			if ok {
				path := filepath.Join(projectRootDir, casePath)
				if !builtin.IsFilePathExists(path) {
					return nil, errors.Wrap(code.ReferencedFileNotFound,
						fmt.Sprintf("referenced testcase file not found: %s", path))
				}

				refTestCase := TestCasePath(path)
				tc, err := refTestCase.ToTestCase()
				if err != nil {
					return nil, err
				}
				step.TestCase = tc
			} else {
				testCaseMap, ok := step.TestCase.(map[string]interface{})
				if !ok {
					return nil, errors.Wrap(code.InvalidCaseFormat,
						fmt.Sprintf("referenced testcase should be map or path(string), got %v", step.TestCase))
				}
				tCase := &TCase{}
				err = mapstructure.Decode(testCaseMap, tCase)
				if err != nil {
					return nil, err
				}
				tc, err := tCase.toTestCase()
				if err != nil {
					return nil, err
				}
				step.TestCase = tc
			}
			_, ok = step.TestCase.(*TestCase)
			if !ok {
				return nil, errors.Wrap(code.InvalidCaseFormat,
					fmt.Sprintf("failed to handle referenced testcase, got %v", step.TestCase))
			}
			testCase.TestSteps = append(testCase.TestSteps, &StepTestCaseWithOptionalArgs{
				step: step,
			})
		} else if step.ThinkTime != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepThinkTime{
				step: step,
			})
		} else if step.Request != nil {
			// init upload
			if len(step.Request.Upload) != 0 {
				initUpload(step)
			}
			testCase.TestSteps = append(testCase.TestSteps, &StepRequestWithOptionalArgs{
				step: step,
			})
		} else if step.Transaction != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepTransaction{
				step: step,
			})
		} else if step.Rendezvous != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepRendezvous{
				step: step,
			})
		} else if step.WebSocket != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepWebSocket{
				step: step,
			})
		} else if step.IOS != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepMobile{
				step: step,
			})
		} else if step.Android != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepMobile{
				step: step,
			})
		} else if step.GRPC != nil {
			testCase.TestSteps = append(testCase.TestSteps, &StepGrpc{
				step: step,
			})
		} else {
			log.Warn().Interface("step", step).Msg("[convertTestCase] unexpected step")
		}
	}
	return testCase, nil
}

func convertCompatRequestBody(request *Request) {
	if request != nil && request.Body == nil {
		if request.Json != nil {
			if request.Headers == nil {
				request.Headers = make(map[string]string)
			}
			request.Headers["Content-Type"] = "application/json; charset=utf-8"
			request.Body = request.Json
			request.Json = nil
		} else if request.Data != nil {
			request.Body = request.Data
			request.Data = nil
		}
	}
}

func convertCompatValidator(Validators []interface{}) (err error) {
	for i, iValidator := range Validators {
		if _, ok := iValidator.(Validator); ok {
			continue
		}
		validatorMap := iValidator.(map[string]interface{})
		validator := Validator{}
		iCheck, checkExisted := validatorMap["check"]
		iAssert, assertExisted := validatorMap["assert"]
		iExpect, expectExisted := validatorMap["expect"]
		// validator check priority: Golang > Python engine style
		if checkExisted && assertExisted && expectExisted {
			// Golang engine style
			validator.Check = iCheck.(string)
			validator.Assert = iAssert.(string)
			validator.Expect = iExpect
			if iMsg, msgExisted := validatorMap["msg"]; msgExisted {
				validator.Message = iMsg.(string)
			}
			validator.Check = convertJmespathExpr(validator.Check)
			Validators[i] = validator
			continue
		}
		if len(validatorMap) == 1 {
			// Python engine style
			for assertMethod, iValidatorContent := range validatorMap {
				validatorContent := iValidatorContent.([]interface{})
				if len(validatorContent) > 3 {
					return errors.Wrap(code.InvalidCaseFormat,
						fmt.Sprintf("unexpected validator format: %v", validatorMap))
				}
				validator.Check = validatorContent[0].(string)
				validator.Assert = assertMethod
				validator.Expect = validatorContent[1]
				if len(validatorContent) == 3 {
					validator.Message = validatorContent[2].(string)
				}
			}
			validator.Check = convertJmespathExpr(validator.Check)
			Validators[i] = validator
			continue
		}
		return errors.Wrap(code.InvalidCaseFormat,
			fmt.Sprintf("unexpected validator format: %v", validatorMap))
	}
	return nil
}

// convertExtract deals with extract expr including hyphen
func convertExtract(extract map[string]string) {
	for key, value := range extract {
		extract[key] = convertJmespathExpr(value)
	}
}

// convertJmespathExpr deals with limited jmespath expression conversion
func convertJmespathExpr(checkExpr string) string {
	if strings.Contains(checkExpr, textExtractorSubRegexp) {
		return checkExpr
	}
	checkItems := strings.Split(checkExpr, ".")
	for i, checkItem := range checkItems {
		checkItem = strings.Trim(checkItem, "\"")
		lowerItem := strings.ToLower(checkItem)
		if strings.HasPrefix(lowerItem, "content-") || lowerItem == "user-agent" {
			checkItems[i] = fmt.Sprintf("\"%s\"", checkItem)
		}
	}
	return strings.Join(checkItems, ".")
}
