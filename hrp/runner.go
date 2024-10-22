package hrp

import (
	"crypto/tls"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/httprunner/funplugin"
	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/sdk"
	"github.com/httprunner/httprunner/v4/hrp/internal/version"
	"github.com/httprunner/httprunner/v4/hrp/pkg/uixt"
)

// Run starts to run testcase with default configs.
func Run(t *testing.T, testcases ...ITestCase) error {
	err := NewRunner(t).SetSaveTests(true).Run(testcases...)
	code.GetErrorCode(err)
	return err
}

// NewRunner constructs a new runner instance.
func NewRunner(t *testing.T) *HRPRunner {
	if t == nil {
		t = &testing.T{}
	}
	jar, _ := cookiejar.New(nil)
	interruptSignal := make(chan os.Signal, 1)
	signal.Notify(interruptSignal, syscall.SIGTERM, syscall.SIGINT)
	return &HRPRunner{
		t:             t,
		failfast:      true, // default to failfast
		genHTMLReport: false,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Jar:     jar, // insert response cookies into request
			Timeout: 120 * time.Second,
		},
		http2Client: &http.Client{
			Transport: &http2.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Jar:     jar, // insert response cookies into request
			Timeout: 120 * time.Second,
		},
		// use default handshake timeout (no timeout limit) here, enable timeout at step level
		wsDialer: &websocket.Dialer{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		caseTimeoutTimer: time.NewTimer(time.Hour * 2), // default case timeout to 2 hour
		interruptSignal:  interruptSignal,
	}
}

type HRPRunner struct {
	t                *testing.T
	failfast         bool
	httpStatOn       bool
	requestsLogOn    bool
	pluginLogOn      bool
	venv             string
	saveTests        bool
	genHTMLReport    bool
	httpClient       *http.Client
	http2Client      *http.Client
	wsDialer         *websocket.Dialer
	caseTimeoutTimer *time.Timer    // case timeout timer
	interruptSignal  chan os.Signal // interrupt signal channel
}

// SetClientTransport configures transport of http client for high concurrency load testing
func (r *HRPRunner) SetClientTransport(maxConns int, disableKeepAlive bool, disableCompression bool) *HRPRunner {
	log.Info().
		Int("maxConns", maxConns).
		Bool("disableKeepAlive", disableKeepAlive).
		Bool("disableCompression", disableCompression).
		Msg("[init] SetClientTransport")
	r.httpClient.Transport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DialContext:         (&net.Dialer{}).DialContext,
		MaxIdleConns:        0,
		MaxIdleConnsPerHost: maxConns,
		DisableKeepAlives:   disableKeepAlive,
		DisableCompression:  disableCompression,
	}
	r.http2Client.Transport = &http2.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		DisableCompression: disableCompression,
	}
	r.wsDialer.EnableCompression = !disableCompression
	return r
}

// SetFailfast configures whether to stop running when one step fails.
func (r *HRPRunner) SetFailfast(failfast bool) *HRPRunner {
	log.Info().Bool("failfast", failfast).Msg("[init] SetFailfast")
	r.failfast = failfast
	return r
}

// SetRequestsLogOn turns on request & response details logging.
func (r *HRPRunner) SetRequestsLogOn() *HRPRunner {
	log.Info().Msg("[init] SetRequestsLogOn")
	r.requestsLogOn = true
	return r
}

// SetHTTPStatOn turns on HTTP latency stat.
func (r *HRPRunner) SetHTTPStatOn() *HRPRunner {
	log.Info().Msg("[init] SetHTTPStatOn")
	r.httpStatOn = true
	return r
}

// SetPluginLogOn turns on plugin logging.
func (r *HRPRunner) SetPluginLogOn() *HRPRunner {
	log.Info().Msg("[init] SetPluginLogOn")
	r.pluginLogOn = true
	return r
}

// SetPython3Venv specifies python3 venv.
func (r *HRPRunner) SetPython3Venv(venv string) *HRPRunner {
	log.Info().Str("venv", venv).Msg("[init] SetPython3Venv")
	r.venv = venv
	return r
}

// SetProxyUrl configures the proxy URL, which is usually used to capture HTTP packets for debugging.
func (r *HRPRunner) SetProxyUrl(proxyUrl string) *HRPRunner {
	log.Info().Str("proxyUrl", proxyUrl).Msg("[init] SetProxyUrl")
	p, err := url.Parse(proxyUrl)
	if err != nil {
		log.Error().Err(err).Str("proxyUrl", proxyUrl).Msg("[init] invalid proxyUrl")
		return r
	}
	r.httpClient.Transport = &http.Transport{
		Proxy:           http.ProxyURL(p),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	r.wsDialer.Proxy = http.ProxyURL(p)
	return r
}

// SetRequestTimeout configures global request timeout in seconds.
func (r *HRPRunner) SetRequestTimeout(seconds float32) *HRPRunner {
	log.Info().Float32("timeout_seconds", seconds).Msg("[init] SetRequestTimeout")
	r.httpClient.Timeout = time.Duration(seconds*1000) * time.Millisecond
	return r
}

// SetCaseTimeout configures global testcase timeout in seconds.
func (r *HRPRunner) SetCaseTimeout(seconds float32) *HRPRunner {
	log.Info().Float32("timeout_seconds", seconds).Msg("[init] SetCaseTimeout")
	r.caseTimeoutTimer = time.NewTimer(time.Duration(seconds*1000) * time.Millisecond)
	return r
}

// SetSaveTests configures whether to save summary of tests.
func (r *HRPRunner) SetSaveTests(saveTests bool) *HRPRunner {
	log.Info().Bool("saveTests", saveTests).Msg("[init] SetSaveTests")
	r.saveTests = saveTests
	return r
}

// GenHTMLReport configures whether to gen html report of api tests.
func (r *HRPRunner) GenHTMLReport() *HRPRunner {
	log.Info().Bool("genHTMLReport", true).Msg("[init] SetgenHTMLReport")
	r.genHTMLReport = true
	return r
}

// Run starts to execute one or multiple testcases.
func (r *HRPRunner) Run(testcases ...ITestCase) (err error) {
	log.Info().Str("hrp_version", version.VERSION).Msg("start running")

	startTime := time.Now()
	defer func() {
		// report run event
		sdk.SendGA4Event("hrp_run", map[string]interface{}{
			"success":              err == nil,
			"engagement_time_msec": time.Since(startTime).Milliseconds(),
		})
	}()

	// record execution data to summary
	s := newOutSummary()

	// load all testcases
	testCases, err := LoadTestCases(testcases...)
	if err != nil {
		log.Error().Err(err).Msg("failed to load testcases")
		return err
	}

	// quit all plugins
	defer func() {
		pluginMap.Range(func(key, value interface{}) bool {
			if plugin, ok := value.(funplugin.IPlugin); ok {
				plugin.Quit()
			}
			return true
		})
	}()

	var runErr error
	// run testcase one by one
	for _, testcase := range testCases {
		// each testcase has its own case runner
		caseRunner, err := r.NewCaseRunner(testcase)
		if err != nil {
			log.Error().Err(err).Msg("[Run] init case runner failed")
			return err
		}

		// release UI driver session
		defer func() {
			for _, client := range caseRunner.uiClients {
				client.Driver.DeleteSession()
			}
		}()

		for it := caseRunner.parametersIterator; it.HasNext(); {
			// case runner can run multiple times with different parameters
			// each run has its own session runner
			sessionRunner := caseRunner.NewSession()
			err1 := sessionRunner.Start(it.Next())
			if err1 != nil {
				log.Error().Err(err1).Msg("[Run] run testcase failed")
				runErr = err1
			}
			caseSummary, err2 := sessionRunner.GetSummary()
			s.appendCaseSummary(caseSummary)
			if err2 != nil {
				log.Error().Err(err2).Msg("[Run] get summary failed")
				if err1 != nil {
					runErr = errors.Wrap(err1, err2.Error())
				} else {
					runErr = err2
				}
			}

			if runErr != nil && r.failfast {
				break
			}
		}
	}
	s.Time.Duration = time.Since(s.Time.StartAt).Seconds()

	// save summary
	if r.saveTests {
		if err := s.genSummary(); err != nil {
			return err
		}
	}

	// generate HTML report
	if r.genHTMLReport {
		if err := s.genHTMLReport(); err != nil {
			return err
		}
	}

	return runErr
}

// NewCaseRunner creates a new case runner for testcase.
// each testcase has its own case runner
func (r *HRPRunner) NewCaseRunner(testcase *TestCase) (*CaseRunner, error) {
	caseRunner := &CaseRunner{
		testCase:  testcase,
		hrpRunner: r,
		parser:    newParser(),
	}

	// init parser plugin
	plugin, err := initPlugin(testcase.Config.Path, r.venv, r.pluginLogOn)
	if err != nil {
		return nil, errors.Wrap(err, "init plugin failed")
	}
	if plugin != nil {
		caseRunner.parser.plugin = plugin
		caseRunner.rootDir = filepath.Dir(plugin.Path())
	}

	// parse testcase config
	if err := caseRunner.parseConfig(); err != nil {
		return nil, errors.Wrap(err, "parse testcase config failed")
	}

	// set request timeout in seconds
	if testcase.Config.RequestTimeout != 0 {
		r.SetRequestTimeout(testcase.Config.RequestTimeout)
	}
	// set testcase timeout in seconds
	if testcase.Config.CaseTimeout != 0 {
		r.SetCaseTimeout(testcase.Config.CaseTimeout)
	}

	// load plugin info to testcase config
	if plugin != nil {
		pluginPath, _ := locatePlugin(testcase.Config.Path)
		if caseRunner.parsedConfig.PluginSetting == nil {
			pluginContent, err := builtin.ReadFile(pluginPath)
			if err != nil {
				return nil, err
			}
			tp := strings.Split(plugin.Path(), ".")
			caseRunner.parsedConfig.PluginSetting = &PluginConfig{
				Path:    pluginPath,
				Content: pluginContent,
				Type:    tp[len(tp)-1],
			}
		}
	}

	return caseRunner, nil
}

type CaseRunner struct {
	hrpRunner *HRPRunner // all case runners share one HRPRunner

	testCase *TestCase // each testcase init its own CaseRunner
	parser   *Parser   // each CaseRunner init its own Parser

	parsedConfig       *TConfig
	parametersIterator *ParametersIterator
	rootDir            string                     // project root dir
	uiClients          map[string]*uixt.DriverExt // UI automation clients for iOS and Android, key is udid/serial
}

// parseConfig parses testcase config, stores to parsedConfig.
func (r *CaseRunner) parseConfig() error {
	cfg := r.testCase.Config

	r.parsedConfig = &TConfig{}
	// deep copy config to avoid data racing
	if err := copier.Copy(r.parsedConfig, cfg); err != nil {
		log.Error().Err(err).Msg("copy testcase config failed")
		return err
	}

	// parse config variables
	parsedVariables, err := r.parser.ParseVariables(cfg.Variables)
	if err != nil {
		log.Error().Interface("variables", cfg.Variables).Err(err).Msg("parse config variables failed")
		return err
	}
	r.parsedConfig.Variables = parsedVariables

	// parse config name
	parsedName, err := r.parser.ParseString(cfg.Name, parsedVariables)
	if err != nil {
		return errors.Wrap(err, "parse config name failed")
	}
	r.parsedConfig.Name = convertString(parsedName)

	// parse config base url
	parsedBaseURL, err := r.parser.ParseString(cfg.BaseURL, parsedVariables)
	if err != nil {
		return errors.Wrap(err, "parse config base url failed")
	}
	r.parsedConfig.BaseURL = convertString(parsedBaseURL)

	// merge config environment variables with base_url
	// priority: env base_url > base_url
	// since deep copy above, assigning here is unnecessary.
	if cfg.Environs == nil {
		r.parsedConfig.Environs = make(map[string]string)
	}
	if value, ok := r.parsedConfig.Environs["base_url"]; !ok || value == "" {
		if r.parsedConfig.BaseURL != "" {
			r.parsedConfig.Variables["base_url"] = r.parsedConfig.BaseURL
		}
	}

	// merge config variables with environment variables
	// priority: env > config variables
	for k, v := range r.parsedConfig.Environs {
		r.parsedConfig.Variables[k] = v
	}

	// ensure correction of think time config
	r.parsedConfig.ThinkTimeSetting.checkThinkTime()

	// ensure correction of websocket config
	r.parsedConfig.WebSocketSetting.checkWebSocket()

	// parse testcase config parameters
	parametersIterator, err := r.parser.initParametersIterator(r.parsedConfig)
	if err != nil {
		log.Error().Err(err).
			Interface("parameters", r.parsedConfig.Parameters).
			Interface("parametersSetting", r.parsedConfig.ParametersSetting).
			Msg("parse config parameters failed")
		return errors.Wrap(err, "parse testcase config parameters failed")
	}
	r.parametersIterator = parametersIterator

	// init iOS/Android clients
	if r.uiClients == nil {
		r.uiClients = make(map[string]*uixt.DriverExt)
	}
	for _, iosDeviceConfig := range r.parsedConfig.IOS {
		if iosDeviceConfig.UDID != "" {
			udid, err := r.parser.ParseString(iosDeviceConfig.UDID, parsedVariables)
			if err != nil {
				return errors.Wrap(err, "failed to parse ios device udid")
			}
			iosDeviceConfig.UDID = udid.(string)
		}

		device, err := uixt.NewIOSDevice(uixt.GetIOSDeviceOptions(iosDeviceConfig)...)
		if err != nil {
			return errors.Wrap(err, "init iOS device failed")
		}
		client, err := device.NewDriver(uixt.WithDriverPlugin(r.parser.plugin))
		if err != nil {
			return errors.Wrap(err, "init iOS WDA client failed")
		}
		r.uiClients[device.UDID] = client
	}
	for _, androidDeviceConfig := range r.parsedConfig.Android {
		if androidDeviceConfig.SerialNumber != "" {
			sn, err := r.parser.ParseString(androidDeviceConfig.SerialNumber, parsedVariables)
			if err != nil {
				return errors.Wrap(err, "failed to parse android device serial")
			}
			androidDeviceConfig.SerialNumber = sn.(string)
		}

		device, err := uixt.NewAndroidDevice(uixt.GetAndroidDeviceOptions(androidDeviceConfig)...)
		if err != nil {
			return errors.Wrap(err, "init Android device failed")
		}
		client, err := device.NewDriver(uixt.WithDriverPlugin(r.parser.plugin))
		if err != nil {
			return errors.Wrap(err, "init Android client failed")
		}
		r.uiClients[device.SerialNumber] = client
	}

	return nil
}

// each boomer task initiates a new session
// in order to avoid data racing
func (r *CaseRunner) NewSession() *SessionRunner {
	sessionRunner := &SessionRunner{
		caseRunner: r,
	}
	sessionRunner.resetSession()
	return sessionRunner
}

// SessionRunner is used to run testcase and its steps.
// each testcase has its own SessionRunner instance and share session variables.
type SessionRunner struct {
	caseRunner *CaseRunner // all session runners share one CaseRunner

	sessionVariables map[string]interface{}
	// transactions stores transaction timing info.
	// key is transaction name, value is map of transaction type and time, e.g. start time and end time.
	transactions      map[string]map[transactionType]time.Time
	startTime         time.Time                  // record start time of the testcase
	summary           *TestCaseSummary           // record test case summary
	wsConnMap         map[string]*websocket.Conn // save all websocket connections
	inheritWsConnMap  map[string]*websocket.Conn // inherit all websocket connections
	pongResponseChan  chan string                // channel used to receive pong response message
	closeResponseChan chan *wsCloseRespObject    // channel used to receive close response message
}

func (r *SessionRunner) resetSession() {
	log.Info().Msg("reset session runner")
	r.sessionVariables = make(map[string]interface{})
	r.transactions = make(map[string]map[transactionType]time.Time)
	r.startTime = time.Now()
	r.summary = newSummary()
	r.wsConnMap = make(map[string]*websocket.Conn)
	r.inheritWsConnMap = make(map[string]*websocket.Conn)
	r.pongResponseChan = make(chan string, 1)
	r.closeResponseChan = make(chan *wsCloseRespObject, 1)
}

func (r *SessionRunner) inheritConnection(src *SessionRunner) {
	log.Info().Msg("inherit session runner")
	r.inheritWsConnMap = make(map[string]*websocket.Conn, len(src.wsConnMap)+len(src.inheritWsConnMap))
	for k, v := range src.wsConnMap {
		r.inheritWsConnMap[k] = v
	}
	for k, v := range src.inheritWsConnMap {
		r.inheritWsConnMap[k] = v
	}
}

// Start runs the test steps in sequential order.
// givenVars is used for data driven
func (r *SessionRunner) Start(givenVars map[string]interface{}) error {
	// report GA event
	sdk.SendGA4Event("hrp_session_runner_start", nil)

	config := r.caseRunner.testCase.Config
	log.Info().Str("testcase", config.Name).Msg("run testcase start")

	// update config variables with given variables
	r.InitWithParameters(givenVars)

	defer func() {
		// close session resource after all steps done or fast fail
		r.releaseResources()
	}()

	// run step in sequential order
	for _, step := range r.caseRunner.testCase.TestSteps {
		select {
		case <-r.caseRunner.hrpRunner.caseTimeoutTimer.C:
			log.Warn().Msg("timeout in session runner")
			return errors.Wrap(code.TimeoutError, "session runner timeout")
		case <-r.caseRunner.hrpRunner.interruptSignal:
			log.Warn().Msg("interrupted in session runner")
			return errors.Wrap(code.InterruptError, "session runner interrupted")
		default:
			// TODO: parse step struct
			// parse step name
			parsedName, err := r.caseRunner.parser.ParseString(step.Name(), r.sessionVariables)
			if err != nil {
				parsedName = step.Name()
			}
			stepName := convertString(parsedName)
			stepType := string(step.Type())
			log.Info().Str("step", stepName).Str("type", stepType).Msg("run step start")
			stepStartTime := time.Now()

			// run times of step
			loopTimes := step.Struct().Loops
			if loopTimes < 0 {
				log.Warn().Int("loops", loopTimes).Msg("loop times should be positive, set to 1")
				loopTimes = 1
			} else if loopTimes == 0 {
				loopTimes = 1
			} else if loopTimes > 1 {
				log.Info().Int("loops", loopTimes).Msg("run step with specified loop times")
			}

			// run step with specified loop times
			var stepResult *StepResult
			for i := 1; i <= loopTimes; i++ {
				var loopIndex string
				if loopTimes > 1 {
					log.Info().Int("index", i).Msg("start running step in loop")
					loopIndex = fmt.Sprintf("_loop_%d", i)
				}

				// run step
				startTime := time.Now().Unix()
				stepResult, err = step.Run(r)
				stepResult.Name = stepName + loopIndex
				stepResult.StartTime = startTime

				r.updateSummary(stepResult)
			}

			// update extracted variables
			for k, v := range stepResult.ExportVars {
				r.sessionVariables[k] = v
			}

			stepElapsed := time.Since(stepStartTime).Milliseconds()
			if err == nil {
				log.Info().Str("step", stepName).
					Str("type", stepType).
					Bool("success", true).
					Int64("elapsed(ms)", stepElapsed).
					Interface("exportVars", stepResult.ExportVars).
					Msg("run step end")
				continue
			}

			// failed
			log.Error().Err(err).Str("step", stepName).
				Str("type", stepType).
				Bool("success", false).
				Int64("elapsed(ms)", stepElapsed).
				Msg("run step end")

			// interrupted or timeout, abort running
			if errors.Is(err, code.InterruptError) || errors.Is(err, code.TimeoutError) {
				return err
			}

			// check if failfast
			if r.caseRunner.hrpRunner.failfast {
				return errors.Wrap(err, "abort running due to failfast setting")
			}
		}
	}

	log.Info().Str("testcase", config.Name).Msg("run testcase end")
	return nil
}

// ParseStepVariables merges step variables with config variables and session variables
func (r *SessionRunner) ParseStepVariables(stepVariables map[string]interface{}) (map[string]interface{}, error) {
	// override variables
	// step variables > session variables (extracted variables from previous steps)
	overrideVars := mergeVariables(stepVariables, r.sessionVariables)
	// step variables > testcase config variables
	overrideVars = mergeVariables(overrideVars, r.caseRunner.parsedConfig.Variables)

	// parse step variables
	parsedVariables, err := r.caseRunner.parser.ParseVariables(overrideVars)
	if err != nil {
		log.Error().Interface("variables", r.caseRunner.parsedConfig.Variables).
			Err(err).Msg("parse step variables failed")
		return nil, errors.Wrap(err, "parse step variables failed")
	}
	return parsedVariables, nil
}

func (r *SessionRunner) ParseStepValidators(iValidators []interface{}, stepVariables map[string]interface{}) ([]interface{}, error) {
	var parsedValidators []interface{}
	var err error
	for _, iValidator := range iValidators {
		validator, ok := iValidator.(Validator)
		if !ok {
			return nil, errors.New("validator type error")
		}
		validator.Expect, err = r.caseRunner.parser.Parse(validator.Expect, stepVariables)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse validator expect")
		}
		parsedValidators = append(parsedValidators, validator)
	}
	return parsedValidators, nil
}

// InitWithParameters updates session variables with given parameters.
// this is used for data driven
func (r *SessionRunner) InitWithParameters(parameters map[string]interface{}) {
	if len(parameters) == 0 {
		return
	}

	log.Info().Interface("parameters", parameters).Msg("update session variables")
	for k, v := range parameters {
		r.sessionVariables[k] = v
	}
}

func (r *SessionRunner) GetSummary() (*TestCaseSummary, error) {
	caseSummary := r.summary
	caseSummary.Name = r.caseRunner.parsedConfig.Name
	caseSummary.Time.StartAt = r.startTime
	caseSummary.Time.Duration = time.Since(r.startTime).Seconds()
	exportVars := make(map[string]interface{})
	for _, value := range r.caseRunner.parsedConfig.Export {
		exportVars[value] = r.sessionVariables[value]
	}
	caseSummary.InOut.ExportVars = exportVars
	caseSummary.InOut.ConfigVars = r.caseRunner.parsedConfig.Variables

	for uuid, client := range r.caseRunner.uiClients {
		// add WDA/UIA logs to summary
		logs := map[string]interface{}{
			"uuid": uuid,
		}

		if client.Device.LogEnabled() {
			log, err := client.Driver.StopCaptureLog()
			if err != nil {
				return caseSummary, err
			}
			logs["content"] = log
		}

		// stop performance monitor
		logs["performance"] = client.Device.StopPerf()
		logs["pcap"] = client.Device.StopPcap()

		caseSummary.Logs = append(caseSummary.Logs, logs)
	}

	return caseSummary, nil
}

// updateSummary updates summary of StepResult.
func (r *SessionRunner) updateSummary(stepResult *StepResult) {
	switch stepResult.StepType {
	case stepTypeTestCase:
		// record requests of testcase step
		if records, ok := stepResult.Data.([]*StepResult); ok {
			for _, result := range records {
				r.addSingleStepResult(result)
			}
		} else {
			r.addSingleStepResult(stepResult)
		}
	default:
		r.addSingleStepResult(stepResult)
	}
}

func (r *SessionRunner) addSingleStepResult(stepResult *StepResult) {
	// update summary
	r.summary.Records = append(r.summary.Records, stepResult)
	r.summary.Stat.Total += 1
	if stepResult.Success {
		r.summary.Stat.Successes += 1
	} else {
		r.summary.Stat.Failures += 1
		// update summary result to failed
		r.summary.Success = false
	}
}

// releaseResources releases resources used by session runner
func (r *SessionRunner) releaseResources() {
	// close websocket connections
	for _, wsConn := range r.wsConnMap {
		if wsConn != nil {
			log.Info().Str("testcase", r.caseRunner.testCase.Config.Name).Msg("websocket disconnected")
			err := wsConn.Close()
			if err != nil {
				log.Error().Err(err).Msg("websocket disconnection failed")
			}
		}
	}
}

func (r *SessionRunner) getWsClient(url string) *websocket.Conn {
	if client, ok := r.wsConnMap[url]; ok {
		return client
	}

	if client, ok := r.inheritWsConnMap[url]; ok {
		return client
	}

	return nil
}
