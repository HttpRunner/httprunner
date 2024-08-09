package hrp

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/mitchellh/mapstructure"

	"github.com/httprunner/httprunner/v4/hrp/internal/json"
	"github.com/httprunner/httprunner/v4/hrp/pkg/boomer"
)

const jsonContentType = "application/json; encoding=utf-8"

func methods(h http.HandlerFunc, methods ...string) http.HandlerFunc {
	methodMap := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		methodMap[m] = struct{}{}
		// GET implies support for HEAD
		if m == "GET" {
			methodMap["HEAD"] = struct{}{}
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := methodMap[r.Method]; !ok {
			http.Error(w, fmt.Sprintf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
			return
		}
		h.ServeHTTP(w, r)
	}
}

func parseBody(r *http.Request) (data map[string]interface{}, err error) {
	if r.Body == nil {
		return nil, nil
	}

	// Always set resp.Data to the incoming request body, in case we don't know
	// how to handle the content type
	body, err := io.ReadAll(r.Body)
	if err != nil {
		r.Body.Close()
		return nil, err
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeJSON(w http.ResponseWriter, body []byte, status int) {
	w.Header().Set("Content-Type", jsonContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

type ServerCode int

// server response code
const (
	Success ServerCode = iota
	ParamsError
	ServerError
	StopError
)

// ServerStatus stores http response code and message
type ServerStatus struct {
	Code    ServerCode `json:"code"`
	Message string     `json:"message"`
}

var EnumAPIResponseSuccess = ServerStatus{
	Code:    Success,
	Message: "success",
}

func EnumAPIResponseParamError(errMsg string) ServerStatus {
	return ServerStatus{
		Code:    ParamsError,
		Message: errMsg,
	}
}

func EnumAPIResponseServerError(errMsg string) ServerStatus {
	return ServerStatus{
		Code:    ServerError,
		Message: errMsg,
	}
}

func EnumAPIResponseStopError(errMsg string) ServerStatus {
	return ServerStatus{
		Code:    StopError,
		Message: errMsg,
	}
}

func CustomAPIResponse(errCode ServerCode, errMsg string) ServerStatus {
	return ServerStatus{
		Code:    errCode,
		Message: errMsg,
	}
}

type StartRequestBody struct {
	boomer.Profile `mapstructure:",squash"`
	Worker         string                 `json:"worker,omitempty" yaml:"worker,omitempty" mapstructure:"worker"` // all
	TestCasePath   string                 `json:"testcase-path" yaml:"testcase-path" mapstructure:"testcase-path"`
	Other          map[string]interface{} `mapstructure:",remain"`
}

type RebalanceRequestBody struct {
	boomer.Profile `mapstructure:",squash"`
	Worker         string                 `json:"worker,omitempty" yaml:"worker,omitempty" mapstructure:"worker"`
	Other          map[string]interface{} `mapstructure:",remain"`
}

type StopRequestBody struct {
	Worker string `json:"worker"`
}

type QuitRequestBody struct {
	Worker string `json:"worker"`
}

type CommonResponseBody struct {
	ServerStatus
}

type APIGetWorkersRequestBody struct{}

type APIGetWorkersResponseBody struct {
	ServerStatus
	Data []boomer.WorkerNode `json:"data"`
}

type APIGetMasterRequestBody struct{}

type APIGetMasterResponseBody struct {
	ServerStatus
	Data map[string]interface{} `json:"data"`
}

type apiHandler struct {
	boomer *HRPBoomer
}

func (b *HRPBoomer) NewAPIHandler() *apiHandler {
	return &apiHandler{boomer: b}
}

// Index renders an HTML index page
func (api *apiHandler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' www.httprunner.com")
	fmt.Fprintf(w, "Welcome to httprunner page!")
}

func (api *apiHandler) Start(w http.ResponseWriter, r *http.Request) {
	var resp *CommonResponseBody
	var err error
	defer func() {
		if err != nil {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseServerError(err.Error()),
			}
		} else {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseSuccess,
			}
		}
		body, _ := json.Marshal(resp)
		writeJSON(w, body, http.StatusOK)
	}()

	// parse body
	data, err := parseBody(r)
	if err != nil {
		return
	}
	req := StartRequestBody{
		Profile: *boomer.NewProfile(),
	}
	err = mapstructure.Decode(data, &req)
	if err != nil {
		return
	}

	// recognize invalid parameters
	if len(req.Other) > 0 {
		keys := make([]string, 0, len(req.Other))
		for k := range req.Other {
			keys = append(keys, k)
		}
		err = fmt.Errorf("failed to recognize params: %v", keys)
		return
	}

	// parse testcase path
	if req.TestCasePath == "" {
		err = errors.New("missing testcases path")
		return
	}
	paths := strings.Split(req.TestCasePath, ",")

	// set testcase path
	api.boomer.SetTestCasesPath(paths)

	// start boomer with profile
	err = api.boomer.Start(&req.Profile)
}

func (api *apiHandler) ReBalance(w http.ResponseWriter, r *http.Request) {
	var resp *CommonResponseBody
	var err error
	defer func() {
		if err != nil {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseServerError(err.Error()),
			}
		} else {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseSuccess,
			}
		}
		body, _ := json.Marshal(resp)
		writeJSON(w, body, http.StatusOK)
	}()

	// parse body
	data, err := parseBody(r)
	if err != nil {
		return
	}
	req := RebalanceRequestBody{
		Profile: *api.boomer.GetProfile(),
	}
	err = mapstructure.Decode(data, &req)
	if err != nil {
		return
	}

	// recognize invalid parameters
	if len(req.Other) > 0 {
		keys := make([]string, 0, len(req.Other))
		for k := range req.Other {
			keys = append(keys, k)
		}
		err = fmt.Errorf("failed to recognize params: %v", keys)
		return
	}

	// rebalance boomer with profile
	err = api.boomer.ReBalance(&req.Profile)
}

func (api *apiHandler) Stop(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{}
	args := r.URL.Query()
	for k, vs := range args {
		for _, v := range vs {
			data[k] = v
		}
	}

	var resp *CommonResponseBody
	var err error
	defer func() {
		if err != nil {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseStopError(err.Error()),
			}
		} else {
			resp = &CommonResponseBody{
				ServerStatus: EnumAPIResponseSuccess,
			}
		}
		body, _ := json.Marshal(resp)
		writeJSON(w, body, http.StatusOK)
	}()

	// stop boomer
	err = api.boomer.Stop()
}

func (api *apiHandler) Quit(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{}
	args := r.URL.Query()
	for k, vs := range args {
		for _, v := range vs {
			data[k] = v
		}
	}
	defer func() {
		resp := &CommonResponseBody{
			ServerStatus: EnumAPIResponseSuccess,
		}
		body, _ := json.Marshal(resp)
		writeJSON(w, body, http.StatusOK)
	}()

	// quit boomer
	api.boomer.Quit()
}

func (api *apiHandler) GetWorkersInfo(w http.ResponseWriter, r *http.Request) {
	resp := &APIGetWorkersResponseBody{
		ServerStatus: EnumAPIResponseSuccess,
		Data:         api.boomer.GetWorkersInfo(),
	}

	body, _ := json.Marshal(resp)
	writeJSON(w, body, http.StatusOK)
}

func (api *apiHandler) GetMasterInfo(w http.ResponseWriter, r *http.Request) {
	resp := &APIGetMasterResponseBody{
		ServerStatus: EnumAPIResponseSuccess,
		Data:         api.boomer.GetMasterInfo(),
	}

	body, _ := json.Marshal(resp)
	writeJSON(w, body, http.StatusOK)
}

func (api *apiHandler) Metrics() http.HandlerFunc {
	registry := boomer.GetRegistry(true)
	h := promhttp.InstrumentMetricHandler(
		registry, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	)
	return func(w http.ResponseWriter, r *http.Request) {
		api.boomer.SetPrometheusMetrics()
		h.ServeHTTP(w, r)
	}
}

func (api *apiHandler) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", methods(api.Index, "GET"))
	mux.HandleFunc("/start", methods(api.Start, "POST"))
	mux.HandleFunc("/rebalance", methods(api.ReBalance, "POST"))
	mux.HandleFunc("/stop", methods(api.Stop, "GET"))
	mux.HandleFunc("/quit", methods(api.Quit, "GET"))
	mux.HandleFunc("/workers", methods(api.GetWorkersInfo, "GET"))
	mux.HandleFunc("/master", methods(api.GetMasterInfo, "GET"))
	mux.HandleFunc("/metrics", api.Metrics())

	return mux
}

func (*apiHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func (b *HRPBoomer) StartServer(ctx context.Context, addr string) {
	h := b.NewAPIHandler()
	mux := h.Handler()

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-b.GetCloseChan():
		}
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatal("shutdown server:", err)
		}
	}()

	log.Printf("starting HTTP server (%v), please use the API to control master", server.Addr)
	err := server.ListenAndServe()
	if err != nil {
		if err == http.ErrServerClosed {
			log.Print("server closed under request")
		} else {
			log.Fatal("server closed unexpected")
		}
	}
}
