package boomer

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/httprunner/httprunner/v4/hrp/internal/json"
)

type transaction struct {
	name        string
	success     bool
	elapsedTime int64
	contentSize int64
}

type requestSuccess struct {
	requestType    string
	name           string
	responseTime   int64
	responseLength int64
}

type requestFailure struct {
	requestType  string
	name         string
	responseTime int64
	errMsg       string
}

type requestStats struct {
	entries            map[string]*statsEntry
	errors             map[string]*statsError
	total              *statsEntry
	totalForTestResult *statsEntry
	startTime          int64

	transactionChan                chan *transaction
	transactionPassed              int64 // accumulated number of passed transactions
	transactionFailed              int64 // accumulated number of failed transactions
	transactionPassedForTestResult int64 // accumulated number of passed transactions
	transactionFailedForTestResult int64 // accumulated number of failed transactions

	requestSuccessChan chan *requestSuccess
	requestFailureChan chan *requestFailure

	statsToMasterChan chan map[string]interface{}
}

func newRequestStats() (stats *requestStats) {
	entries := make(map[string]*statsEntry)
	errors := make(map[string]*statsError)

	stats = &requestStats{
		entries: entries,
		errors:  errors,
	}
	stats.transactionChan = make(chan *transaction, 100)
	stats.requestSuccessChan = make(chan *requestSuccess, 100)
	stats.requestFailureChan = make(chan *requestFailure, 100)
	stats.statsToMasterChan = make(chan map[string]interface{}, 100)

	stats.total = &statsEntry{
		Name:   "Total",
		Method: "",
	}
	stats.totalForTestResult = &statsEntry{
		Name:   "Total",
		Method: "",
	}
	stats.total.reset()
	stats.totalForTestResult.reset()

	return stats
}

func (s *requestStats) logTransaction(name string, success bool, responseTime int64, contentLength int64) {
	if success {
		s.transactionPassed++
	} else {
		s.transactionFailed++
		s.get(name, "transaction").logFailures()
	}
	s.get(name, "transaction").log(responseTime, contentLength)
}

func (s *requestStats) logRequest(method, name string, responseTime int64, contentLength int64) {
	if method != "testcase" {
		s.total.log(responseTime, contentLength)
	}
	s.get(name, method).log(responseTime, contentLength)
}

func (s *requestStats) logError(method, name, err string) {
	if method != "testcase" {
		s.total.logFailures()
	}
	s.get(name, method).logFailures()

	// store error in errors map
	key := genMD5(method, name, err)
	entry, ok := s.errors[key]
	if !ok {
		entry = &statsError{
			name:   name,
			method: method,
			errMsg: err,
		}
		s.errors[key] = entry
	}
	entry.occured()
}

func (s *requestStats) get(name string, method string) (entry *statsEntry) {
	entry, ok := s.entries[name+method]
	if !ok {
		newEntry := &statsEntry{
			Name:          name,
			Method:        method,
			ResponseTimes: make(map[int64]int64),
		}
		s.entries[name+method] = newEntry
		return newEntry
	}
	return entry
}

func (s *requestStats) clearAll() {
	s.total = &statsEntry{
		Name:   "Total",
		Method: "",
	}
	s.total.reset()
	s.totalForTestResult = &statsEntry{
		Name:   "Total",
		Method: "",
	}
	s.totalForTestResult.reset()
	s.transactionPassed = 0
	s.transactionFailed = 0
	s.transactionPassedForTestResult, s.transactionFailedForTestResult = 0, 0
	s.entries = make(map[string]*statsEntry)
	s.errors = make(map[string]*statsError)
	s.startTime = time.Now().Unix()
}

func (s *requestStats) serializeStats() []interface{} {
	entries := make([]interface{}, 0, len(s.entries))
	for _, v := range s.entries {
		if !(v.NumRequests == 0 && v.NumFailures == 0) {
			entries = append(entries, v.getStrippedReport())
		}
	}
	return entries
}

func (s *requestStats) serializeErrors() map[string]map[string]interface{} {
	errors := make(map[string]map[string]interface{})
	for k, v := range s.errors {
		errors[k] = v.toMap()
	}
	return errors
}

func (s *requestStats) collectReportData() map[string]interface{} {
	data := make(map[string]interface{})
	data["transactions"] = map[string]int64{
		"passed": s.transactionPassed,
		"failed": s.transactionFailed,
	}
	s.transactionPassedForTestResult += s.transactionPassed
	s.transactionFailedForTestResult += s.transactionFailed
	data["stats"] = s.serializeStats()
	s.totalForTestResult.extend(s.total)
	data["stats_total"] = s.total.getStrippedReport()
	data["errors"] = s.serializeErrors()
	// reset transactions
	s.transactionPassed, s.transactionFailed = 0, 0
	s.errors = make(map[string]*statsError)
	return data
}

// statsEntry represents a single stats entry (name and method)
type statsEntry struct {
	// Name (URL) of this stats entry
	Name string `json:"name"`
	// Method (GET, POST, PUT, etc.)
	Method string `json:"method"`
	// The number of requests made
	NumRequests int64 `json:"num_requests"`
	// Number of failed request
	NumFailures int64 `json:"num_failures"`
	// Total sum of the response times
	TotalResponseTime int64 `json:"total_response_time"`
	// Minimum response time
	MinResponseTime int64 `json:"min_response_time"`
	// Maximum response time
	MaxResponseTime int64 `json:"max_response_time"`
	// A {response_time => count} dict that holds the response time distribution of all the requests
	// The keys (the response time in ms) are rounded to store 1, 2, ... 9, 10, 20. .. 90,
	// 100, 200 .. 900, 1000, 2000 ... 9000, in order to save memory.
	// This dict is used to calculate the median and percentile response times.
	ResponseTimes map[int64]int64 `json:"response_times"`
	// The sum of the content length of all the requests for this entry
	TotalContentLength int64 `json:"total_content_length"`
	// Time of the first request for this entry
	StartTime int64 `json:"start_time"`
	// Time of the last request for this entry
	LastRequestTimestamp int64 `json:"last_request_timestamp"`
	// Boomer doesn't allow None response time for requests like locust.
	// num_none_requests is added to keep compatible with locust.
	NumNoneRequests int64 `json:"num_none_requests"`
}

func (s *statsEntry) resetStartTime() {
	atomic.StoreInt64(&s.StartTime, time.Duration(time.Now().UnixNano()).Milliseconds())
}

func (s *statsEntry) reset() {
	atomic.StoreInt64(&s.StartTime, time.Duration(time.Now().UnixNano()).Milliseconds())
	s.NumRequests = 0
	s.NumFailures = 0
	s.TotalResponseTime = 0
	s.ResponseTimes = make(map[int64]int64)
	s.MinResponseTime = 0
	s.MaxResponseTime = 0
	s.LastRequestTimestamp = time.Duration(time.Now().UnixNano()).Milliseconds()
	s.TotalContentLength = 0
}

func (s *statsEntry) log(responseTime int64, contentLength int64) {
	s.NumRequests++

	s.logTimeOfRequest()
	s.logResponseTime(responseTime)

	s.TotalContentLength += contentLength
}

func (s *statsEntry) logTimeOfRequest() {
	s.LastRequestTimestamp = time.Duration(time.Now().UnixNano()).Milliseconds()
}

func (s *statsEntry) logResponseTime(responseTime int64) {
	s.TotalResponseTime += responseTime

	if s.MinResponseTime == 0 {
		s.MinResponseTime = responseTime
	}

	if responseTime < s.MinResponseTime {
		s.MinResponseTime = responseTime
	}

	if responseTime > s.MaxResponseTime {
		s.MaxResponseTime = responseTime
	}

	var roundedResponseTime int64

	// to avoid too much data that has to be transferred to the master node when
	// running in distributed mode, we save the response time rounded in a dict
	// so that 147 becomes 150, 3432 becomes 3400 and 58760 becomes 59000
	// see also locust's stats.py
	if responseTime < 100 {
		roundedResponseTime = responseTime
	} else if responseTime < 1000 {
		roundedResponseTime = int64(round(float64(responseTime), .5, -1))
	} else if responseTime < 10000 {
		roundedResponseTime = int64(round(float64(responseTime), .5, -2))
	} else {
		roundedResponseTime = int64(round(float64(responseTime), .5, -3))
	}

	_, ok := s.ResponseTimes[roundedResponseTime]
	if !ok {
		s.ResponseTimes[roundedResponseTime] = 1
	} else {
		s.ResponseTimes[roundedResponseTime]++
	}
}

func (s *statsEntry) logFailures() {
	s.NumFailures++
}

func (s *statsEntry) serialize() map[string]interface{} {
	var result map[string]interface{}
	val, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	err = json.Unmarshal(val, &result)
	if err != nil {
		return nil
	}
	return result
}

func (s *statsEntry) getStrippedReport() map[string]interface{} {
	report := s.serialize()
	s.reset()
	return report
}

func (s *statsEntry) extend(one *statsEntry) {
	s.NumRequests += one.NumRequests
	s.NumFailures += one.NumFailures
	s.TotalResponseTime += one.TotalResponseTime
	if s.MinResponseTime == 0 {
		s.MinResponseTime = one.MinResponseTime
	}
	s.MinResponseTime = int64(math.Min(float64(s.MinResponseTime), float64(one.MinResponseTime)))
	s.MaxResponseTime = int64(math.Max(float64(s.MaxResponseTime), float64(one.MaxResponseTime)))

	for key, val := range one.ResponseTimes {
		if _, ok := s.ResponseTimes[key]; ok {
			s.ResponseTimes[key] += val
		} else {
			s.ResponseTimes[key] = val
		}
	}

	s.TotalContentLength += one.TotalContentLength
	s.StartTime = int64(math.Min(float64(s.StartTime), float64(one.StartTime)))
	s.LastRequestTimestamp = int64(math.Max(float64(s.LastRequestTimestamp), float64(one.LastRequestTimestamp)))
}

type statsError struct {
	name        string
	method      string
	errMsg      string
	occurrences int64
}

func (err *statsError) occured() {
	err.occurrences++
}

func (err *statsError) toMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["method"] = err.method
	m["name"] = err.name
	m["error"] = err.errMsg
	m["occurrences"] = err.occurrences
	return m
}
func (err *statsError) deserialize(m map[string]interface{}) *statsError {
	err.method = m["method"].(string)
	err.name = m["name"].(string)
	err.errMsg, _ = m["error"].(string)
	err.occurrences = int64(m["occurrences"].(float64))
	return err
}
