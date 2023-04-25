package hrp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/fullstorydev/grpcurl"
	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/pkg/errors"

	"github.com/test-instructor/grpc-plugin/plugin"
	"io"
	"strings"
	"testing"
	"time"
)

// GrpcType Request type
type GrpcType string

var (
	GrpcTypeSimple              GrpcType = "Simple"
	GrpcTypeServerSideStream    GrpcType = "ServerSideStream"
	GrpcTypeClientSideStream    GrpcType = "ClientSideStream"
	GrpcTypeBidirectionalStream GrpcType = "BidirectionalStream"
)

// StepGrpc implements IStep interface.
type StepGrpc struct {
	step *TStep
}

func (g *StepGrpc) Name() string {
	return g.step.Name
}

func (g *StepGrpc) Type() StepType {
	return stepTypeGRPC
}

func (g *StepGrpc) Struct() *TStep {
	return g.step
}

func (g *StepGrpc) Run(r *SessionRunner) (*StepResult, error) {
	return runStepGRPC(r, g.step)
}

func setGrpcErr(g *SessionRunner, e error, start time.Time, rb *grpcBuilder, parser *Parser, name string) (stepResult *StepResult, err error) {
	stepResult = &StepResult{
		Name:        name,
		StepType:    stepTypeGRPC,
		Success:     false,
		ContentSize: 0,
	}
	sessionData := newSessionData()
	sessionData.ReqResps.Request = rb.requestMap
	gResp := &grpcResponse{
		timer:  0,
		result: "",
		err:    e,
		status: statusTypeFailed,
	}
	respObj, _ := newGrpcResponseObject(g.caseRunner.hrpRunner.t, parser, gResp)
	sessionData.ReqResps.Response = builtin.FormatResponse(respObj.respObjMeta)
	stepResult.Data = sessionData
	stepResult.Elapsed = time.Since(start).Milliseconds()
	return stepResult, e
}

func runStepGRPC(g *SessionRunner, step *TStep) (stepResult *StepResult, err error) {

	stepResult = &StepResult{
		Name:        step.Name,
		StepType:    stepTypeGRPC,
		Success:     false,
		ContentSize: 0,
	}

	stepVariables, err := g.ParseStepVariables(step.Variables)
	if err != nil {
		err = errors.Wrap(err, "parse step variables failed")
		return
	}

	parser := g.caseRunner.parser
	config := g.caseRunner.parsedConfig

	rb := newGrpcBuilder(parser, config, step.GRPC)
	err = rb.prepareHeaders(stepVariables)
	if err != nil {
		return
	}
	rb.prepareHost(rb.config.BaseURL)
	rb.prepareURL()
	err = rb.prepareBody(stepVariables)
	if err != nil {
		return
	}

	stepVariables["hrp_step_name"] = step.Name
	stepVariables["hrp_step_request"] = rb.requestMap
	stepVariables["request"] = rb.requestMap
	// deal with setup hooks
	for _, setupHook := range step.SetupHooks {
		req, err := parser.Parse(setupHook, stepVariables)
		if err != nil {
			continue
		}
		reqMap, ok := req.(map[string]interface{})
		if ok && reqMap != nil {
			rb.requestMap = reqMap
			stepVariables["request"] = reqMap
		}
	}
	for _, setupHook := range step.SetupHooks {
		req, err := parser.Parse(setupHook, stepVariables)
		if err != nil {
			continue
		}
		reqMap, ok := req.(map[string]interface{})
		if ok && reqMap != nil {
			rb.requestMap = reqMap
			stepVariables["request"] = reqMap
		}
	}
	if len(step.SetupHooks) > 0 {
		requestBody, ok := rb.requestMap["body"].(map[string]interface{})
		if ok {
			body, err := json.Marshal(requestBody)
			if err == nil {
				rb.stepGrpc.Body = io.NopCloser(bytes.NewReader(body))
				rb.body = io.NopCloser(bytes.NewReader(body))
			}
		}
		requestHeaders, ok := rb.requestMap["headers"].(map[string]interface{})
		if ok {
			requestHeadersNew := make(map[string]string)
			rb.stepGrpc.Headers = make(map[string]string)
			for k, v := range requestHeaders {
				requestHeadersNew[k] = fmt.Sprintf("%v", v)
			}
			rb.stepGrpc.Headers = requestHeadersNew
			err = rb.prepareHeaders(stepVariables)
			if err != nil {
				return
			}
		}
	}

	{
		// 修改测试报告显示的url
		rb.requestMap["url"] = "GRPC://" + rb.Host + "/" + rb.URL

	}
	if step.GRPC.Timeout != 0 {
		to := time.Duration(step.Request.Timeout*1000) * time.Millisecond
		_ = to
	}
	//gResp1 := &plugin.Grpc{}
	gResp := &grpcResponse{}
	start := time.Now()
	ig := plugin.NewInvokeGrpc(&plugin.Grpc{
		Host:     rb.Host,
		Method:   rb.URL,
		Metadata: rb.Metadata,
		Timeout:  rb.Timeout,
		Body:     rb.body,
	})

	switch {
	case step.GRPC.Type == GrpcTypeSimple:
		res, err := ig.InvokeFunction()
		//result, timer, gErr := h.InvokeFunction(rb.URL, rb.body, rb.Headers)
		if err != nil {
			return setGrpcErr(g, err, start, rb, parser, step.Name)
		}
		gResp = &grpcResponse{
			err:     err,
			results: res,
		}
	case step.GRPC.Type == GrpcTypeServerSideStream:
		err = errors.New("没有实现服务端流模式")
		return setGrpcErr(g, err, start, rb, parser, step.Name)
	case step.GRPC.Type == GrpcTypeClientSideStream:
		err = errors.New("没有实现客户端流模式")
		return setGrpcErr(g, err, start, rb, parser, step.Name)
	case step.GRPC.Type == GrpcTypeBidirectionalStream:
		err = errors.New("没有实现双向流模式")
		return setGrpcErr(g, err, start, rb, parser, step.Name)
	default:
		res, err := ig.InvokeFunction()
		//result, timer, gErr := h.InvokeFunction(rb.URL, rb.body, rb.Headers)
		if err != nil {
			return setGrpcErr(g, err, start, rb, parser, step.Name)
		}
		gResp = &grpcResponse{
			err:     err,
			results: res,
		}
	}
	//new response object
	respObj, err := newGrpcResponseObject(g.caseRunner.hrpRunner.t, parser, gResp)
	if err != nil {
		err = errors.Wrap(err, "init ResponseObject error")
		return
	}
	stepResult.Elapsed = time.Since(start).Milliseconds()
	if g.caseRunner.hrpRunner.httpStatOn {
		//grpc Temporarily unavailable
		// resp.Body has been ReadAll
		//httpStat.Finish()
		//stepResult.HttpStat = httpStat.Durations()
		//httpStat.Print()
	}

	stepVariables["hrp_step_response"] = respObj.respObjMeta
	stepVariables["response"] = respObj.respObjMeta

	// deal with teardown hooks
	for _, teardownHook := range step.TeardownHooks {
		res, err := parser.Parse(teardownHook, stepVariables)
		if err != nil {
			continue
		}
		resMpa, ok := res.(map[string]interface{})
		if ok {
			stepVariables["response"] = resMpa
			respObj.respObjMeta = resMpa
		}
	}
	sessionData := newSessionData()
	sessionData.ReqResps.Request = rb.requestMap
	sessionData.ReqResps.Response = builtin.FormatResponse(respObj.respObjMeta)

	// extract variables from response
	extractors := step.Extract
	extractMapping := respObj.Extract(extractors, stepVariables)
	stepResult.ExportVars = extractMapping

	// override step variables with extracted variables
	stepVariables = mergeVariables(stepVariables, extractMapping)

	// validate response
	err = respObj.Validate(step.Validators, stepVariables)
	sessionData.Validators = respObj.validationResults
	if err == nil {
		sessionData.Success = true
		stepResult.Success = true
	}
	stepResult.Data = sessionData

	return stepResult, err
}

type status int

var statusConnectionFailed status = 1
var statusTypeFailed status = 2

var _ = statusConnectionFailed

// response data format
type grpcResponse struct {
	body    string
	timer   time.Duration
	result  string
	err     error
	status  status
	results *plugin.RpcResult
}

// Grpc represents HTTP request data structure.
// This is used for teststep.
type Grpc struct {
	Host          string `json:"host" yaml:"host"`
	URL           string `json:"url" yaml:"url"`
	ContentLength int64
	Headers       map[string]string      `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body          interface{}            `json:"body,omitempty"`
	Timeout       float32                `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Upload        map[string]interface{} `json:"upload,omitempty" yaml:"upload,omitempty"`
	Type          GrpcType               `json:"type,omitempty" yaml:"type,omitempty"`
}

type grpcBuilder struct {
	stepGrpc   *Grpc
	parser     *Parser
	config     *TConfig
	requestMap map[string]interface{}
	formatter  grpcurl.Formatter
	descSource grpcurl.DescriptorSource

	Host     string
	URL      string
	body     io.Reader
	Timeout  float32
	Headers  []string
	Method   string `json:"url" yaml:"url"`
	Metadata []plugin.RpcMetadata
}

func (r *grpcBuilder) prepareHeaders(stepVariables map[string]interface{}) error {
	// prepare request headers
	stepHeaders := r.stepGrpc.Headers
	if r.config.Headers != nil {
		// override headers
		stepHeaders = mergeMap(stepHeaders, r.config.Headers)
	}

	var metadata []string

	if len(stepHeaders) > 0 {
		headers, err := r.parser.ParseHeaders(stepHeaders, stepVariables)
		if err != nil {
			return errors.Wrap(err, "parse headers failed")
		}
		r.Metadata = []plugin.RpcMetadata{}
		for key, value := range headers {
			if strings.HasPrefix(key, ":") {
				continue
			}
			metadata = append(metadata, fmt.Sprintf("%s:%s", key, value))
			Metadata := plugin.RpcMetadata{Name: key, Value: value}
			r.Metadata = append(r.Metadata, Metadata)

		}

	}

	r.requestMap = make(map[string]interface{})
	r.requestMap["headers"] = metadata
	r.Headers = metadata
	return nil
}

func (r *grpcBuilder) prepareHost(h string) {
	if h[:7] == "http://" {
		h = h[7:]
	}
	if h[:8] == "https://" {
		h = h[8:]
	}
	if h[len(h)-1:] == "/" {
		h = h[:len(h)-1]
	}
	r.Host = h
}

func (r *grpcBuilder) prepareURL() {
	url := r.stepGrpc.URL
	urlList := strings.Split(url, "/")
	if len(urlList) == 1 {
		r.URL = urlList[0]
	} else {
		r.URL = urlList[len(urlList)-1]
		r.prepareHost(urlList[0])
	}
}

func (r *grpcBuilder) prepareBody(stepVariables map[string]interface{}) error {
	// prepare request body
	if r.stepGrpc.Body == nil {
		return nil
	}

	data, err := r.parser.Parse(r.stepGrpc.Body, stepVariables)
	if err != nil {
		return err
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	r.requestMap["body"] = data
	r.stepGrpc.Body = io.NopCloser(bytes.NewReader(dataBytes))
	r.body = io.NopCloser(bytes.NewReader(dataBytes))
	return nil
}

func newGrpcBuilder(parser *Parser, config *TConfig, stepGrpc *Grpc) *grpcBuilder {
	var gb grpcBuilder
	gb.config = config
	gb.stepGrpc = stepGrpc
	gb.parser = parser
	gb.Host = config.BaseURL
	gb.URL = stepGrpc.URL

	return &gb
}

type grpcRespObjMeta struct {
	Proto      string            `json:"proto"`
	StatusCode string            `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       interface{}       `json:"body"`
	Err        string            `json:"err"`
}

func newGrpcResponseObject(t *testing.T, parser *Parser, resp *grpcResponse) (*responseObject, error) {
	var statusCode = "OK"
	var errStr string
	var body interface{}
	var headers = make(map[string]string)
	if resp.results != nil {
		if resp.results != nil && resp.results.Error != nil {
			statusCode = resp.results.Error.Name
			errStr = resp.results.Error.Message
		}
		if resp.results != nil && resp.results.Responses != nil {
			body = string(resp.results.Responses[0].Data)
			if err := json.Unmarshal(resp.results.Responses[0].Data, &body); err != nil {
				// response body is not json, use raw body
				body = string(resp.results.Responses[0].Data)
			}
		}
		if resp.results.Headers != nil {
			for _, v := range resp.results.Headers {
				headers[v.Name] = v.Value
			}
		}
	}
	if resp.err != nil {
		statusCode = ""
		errStr = resp.err.Error()
	}

	respObjMeta := grpcRespObjMeta{
		Proto:      "gRPC",
		Err:        errStr,
		Body:       body,
		StatusCode: statusCode,
		Headers:    headers,
	}
	return convertToResponseObject(t, parser, respObjMeta)
}
