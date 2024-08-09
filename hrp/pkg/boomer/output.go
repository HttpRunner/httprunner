package boomer

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/rs/zerolog/log"
	uuid "github.com/satori/go.uuid"

	"github.com/httprunner/httprunner/v4/hrp/internal/json"
)

// Output is primarily responsible for printing test results to different destinations
// such as consoles, files. You can write you own output and add to boomer.
// When running in standalone mode, the default output is ConsoleOutput, you can add more.
// When running in distribute mode, test results will be reported to master with or without
// an output.
// All the OnXXX function will be call in a separated goroutine, just in case some output will block.
// But it will wait for all outputs return to avoid data lost.
type Output interface {
	// OnStart will be call before the test starts.
	OnStart()

	// By default, each output receive stats data from runner every three seconds.
	// OnEvent is responsible for dealing with the data.
	OnEvent(data map[string]interface{})

	// OnStop will be called before the test ends.
	OnStop()
}

// ConsoleOutput is the default output for standalone mode.
type ConsoleOutput struct{}

// NewConsoleOutput returns a ConsoleOutput.
func NewConsoleOutput() *ConsoleOutput {
	return &ConsoleOutput{}
}

func getMedianResponseTime(numRequests int64, responseTimes map[int64]int64) int64 {
	medianResponseTime := int64(0)
	if len(responseTimes) != 0 {
		pos := (numRequests - 1) / 2
		var sortedKeys []int64
		for k := range responseTimes {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Slice(sortedKeys, func(i, j int) bool {
			return sortedKeys[i] < sortedKeys[j]
		})
		for _, k := range sortedKeys {
			if pos < responseTimes[k] {
				medianResponseTime = k
				break
			}
			pos -= responseTimes[k]
		}
	}
	return medianResponseTime
}

func getAvgResponseTime(numRequests int64, totalResponseTime int64) (avgResponseTime float64) {
	avgResponseTime = float64(0)
	if numRequests != 0 {
		avgResponseTime = float64(totalResponseTime) / float64(numRequests)
	}
	return avgResponseTime
}

func getAvgContentLength(numRequests int64, totalContentLength int64) (avgContentLength int64) {
	avgContentLength = int64(0)
	if numRequests != 0 {
		avgContentLength = totalContentLength / numRequests
	}
	return avgContentLength
}

func getCurrentRps(numRequests int64, duration float64) (currentRps float64) {
	currentRps = float64(numRequests) / duration
	return currentRps
}

func getCurrentFailPerSec(numFailures int64, duration float64) (currentFailPerSec float64) {
	currentFailPerSec = float64(numFailures) / duration
	return currentFailPerSec
}

func getTotalFailRatio(totalRequests, totalFailures int64) (failRatio float64) {
	if totalRequests == 0 {
		return 0
	}
	return float64(totalFailures) / float64(totalRequests)
}

// OnStart of ConsoleOutput has nothing to do.
func (o *ConsoleOutput) OnStart() {
}

// OnStop of ConsoleOutput has nothing to do.
func (o *ConsoleOutput) OnStop() {
}

// OnEvent will print to the console.
func (o *ConsoleOutput) OnEvent(data map[string]interface{}) {
	output, err := convertData(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert data")
		return
	}

	state := getStateName(output.State)

	currentTime := time.Now()
	println(fmt.Sprintf("Current time: %s, Users: %d, State: %s, Total RPS: %.1f, Total Average Response Time: %.1fms, Total Fail Ratio: %.1f%%",
		currentTime.Format("2006/01/02 15:04:05"), output.UserCount, state, output.TotalRPS, output.TotalAvgResponseTime, output.TotalFailRatio*100))
	println(fmt.Sprintf("Accumulated Transactions: %d Passed, %d Failed",
		output.TransactionsPassed, output.TransactionsFailed))
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Type", "Name", "# requests", "# fails", "Median", "Average", "Min", "Max", "Content Size", "# reqs/sec", "# fails/sec"})

	for _, stat := range output.Stats {
		row := make([]string, 11)
		row[0] = stat.Method
		row[1] = stat.Name
		row[2] = strconv.FormatInt(stat.NumRequests, 10)
		row[3] = strconv.FormatInt(stat.NumFailures, 10)
		row[4] = strconv.FormatInt(stat.medianResponseTime, 10)
		row[5] = strconv.FormatFloat(stat.avgResponseTime, 'f', 2, 64)
		row[6] = strconv.FormatInt(stat.MinResponseTime, 10)
		row[7] = strconv.FormatInt(stat.MaxResponseTime, 10)
		row[8] = strconv.FormatInt(stat.avgContentLength, 10)
		row[9] = strconv.FormatFloat(stat.currentRps, 'f', 2, 64)
		row[10] = strconv.FormatFloat(stat.currentFailPerSec, 'f', 2, 64)
		table.Append(row)
	}
	table.Render()
	println()
}

type statsEntryOutput struct {
	statsEntry

	medianResponseTime int64   // median response time
	avgResponseTime    float64 // average response time, round float to 2 decimal places
	avgContentLength   int64   // average content size
	currentRps         float64 // # reqs/sec
	currentFailPerSec  float64 // # fails/sec
	duration           float64 // the duration of stats
}

type dataOutput struct {
	UserCount            int64                             `json:"user_count"`
	State                int32                             `json:"state"`
	TotalStats           *statsEntryOutput                 `json:"stats_total"`
	TransactionsPassed   int64                             `json:"transactions_passed"`
	TransactionsFailed   int64                             `json:"transactions_failed"`
	TotalAvgResponseTime float64                           `json:"total_avg_response_time"`
	TotalMinResponseTime float64                           `json:"total_min_response_time"`
	TotalMaxResponseTime float64                           `json:"total_max_response_time"`
	TotalRPS             float64                           `json:"total_rps"`
	TotalFailRatio       float64                           `json:"total_fail_ratio"`
	TotalFailPerSec      float64                           `json:"total_fail_per_sec"`
	Duration             float64                           `json:"duration"`
	Stats                []*statsEntryOutput               `json:"stats"`
	Errors               map[string]map[string]interface{} `json:"errors"`
}

func convertData(data map[string]interface{}) (output *dataOutput, err error) {
	userCount, ok := data["user_count"].(int64)
	if !ok {
		return nil, fmt.Errorf("user_count is not int32")
	}
	state, ok := data["state"].(int32)
	if !ok {
		return nil, fmt.Errorf("state is not int32")
	}
	stats, ok := data["stats"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("stats is not []interface{}")
	}

	errors := data["errors"].(map[string]map[string]interface{})

	transactions, ok := data["transactions"].(map[string]int64)
	if !ok {
		return nil, fmt.Errorf("transactions is not map[string]int64")
	}
	transactionsPassed := transactions["passed"]
	transactionsFailed := transactions["failed"]

	// convert stats in total
	statsTotal, ok := data["stats_total"].(interface{})
	if !ok {
		return nil, fmt.Errorf("stats_total is not interface{}")
	}
	entryTotalOutput, err := deserializeStatsEntry(statsTotal)
	if err != nil {
		return nil, err
	}

	output = &dataOutput{
		UserCount:            userCount,
		State:                state,
		Duration:             entryTotalOutput.duration,
		TotalStats:           entryTotalOutput,
		TransactionsPassed:   transactionsPassed,
		TransactionsFailed:   transactionsFailed,
		TotalAvgResponseTime: entryTotalOutput.avgResponseTime,
		TotalMaxResponseTime: float64(entryTotalOutput.MaxResponseTime),
		TotalMinResponseTime: float64(entryTotalOutput.MinResponseTime),
		TotalRPS:             entryTotalOutput.currentRps,
		TotalFailRatio:       getTotalFailRatio(entryTotalOutput.NumRequests, entryTotalOutput.NumFailures),
		TotalFailPerSec:      entryTotalOutput.currentFailPerSec,
		Stats:                make([]*statsEntryOutput, 0, len(stats)),
		Errors:               errors,
	}

	// convert stats
	for _, stat := range stats {
		entryOutput, err := deserializeStatsEntry(stat)
		if err != nil {
			return nil, err
		}
		output.Stats = append(output.Stats, entryOutput)
	}
	// sort stats by type
	sort.Slice(output.Stats, func(i, j int) bool {
		return output.Stats[i].Method < output.Stats[j].Method
	})
	return
}

func deserializeStatsEntry(stat interface{}) (entryOutput *statsEntryOutput, err error) {
	statBytes, err := json.Marshal(stat)
	if err != nil {
		return nil, err
	}
	entry := statsEntry{}
	if err = json.Unmarshal(statBytes, &entry); err != nil {
		return nil, err
	}
	return convert2StatsEntryOutput(&entry), nil
}

func convert2StatsEntryOutput(entry *statsEntry) *statsEntryOutput {
	var duration float64
	if entry.Name == "Total" {
		duration = float64(entry.LastRequestTimestamp-entry.StartTime) / 1e3
		if duration == 0 {

		}
	} else {
		duration = float64(reportStatsInterval / time.Second)
	}

	numRequests := entry.NumRequests
	entryOutput := &statsEntryOutput{
		statsEntry:         *entry,
		duration:           duration,
		medianResponseTime: getMedianResponseTime(numRequests, entry.ResponseTimes),
		avgResponseTime:    getAvgResponseTime(numRequests, entry.TotalResponseTime),
		avgContentLength:   getAvgContentLength(numRequests, entry.TotalContentLength),
		currentRps:         getCurrentRps(numRequests, duration),
		currentFailPerSec:  getCurrentFailPerSec(entry.NumFailures, duration),
	}
	return entryOutput
}

// gauge vectors for requests
var (
	gaugeNumRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "num_requests",
			Help: "The number of requests",
		},
		[]string{"method", "name"},
	)
	gaugeNumFailures = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "num_failures",
			Help: "The number of failures",
		},
		[]string{"method", "name"},
	)
	gaugeMedianResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "median_response_time",
			Help: "The median response time",
		},
		[]string{"method", "name"},
	)
	gaugeAverageResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "average_response_time",
			Help: "The average response time",
		},
		[]string{"method", "name"},
	)
	gaugeMinResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "min_response_time",
			Help: "The min response time",
		},
		[]string{"method", "name"},
	)
	gaugeMaxResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "max_response_time",
			Help: "The max response time",
		},
		[]string{"method", "name"},
	)
	gaugeAverageContentLength = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "average_content_length",
			Help: "The average content length",
		},
		[]string{"method", "name"},
	)
	gaugeCurrentRPS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "current_rps",
			Help: "The current requests per second",
		},
		[]string{"method", "name"},
	)
	gaugeCurrentFailPerSec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "current_fail_per_sec",
			Help: "The current failure number per second",
		},
		[]string{"method", "name"},
	)
)

// counter for total
var (
	counterErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors",
			Help: "The errors of load testing",
		},
		[]string{"method", "name", "error"},
	)
	counterTotalErrors = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "errors",
			Help: "The errors of load testing",
		},
		[]string{"method", "name", "error"},
	)
	counterTotalNumRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_num_requests",
			Help: "The number of requests in total",
		},
		[]string{"method", "name"},
	)
	counterTotalNumFailures = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_num_failures",
			Help: "The number of failures in total",
		},
		[]string{"method", "name"},
	)
)

// summary for total
var (
	summaryResponseTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "response_time",
			Help: "The summary of response time",
			Objectives: map[float64]float64{
				0.5:  0.01,  // PCT50
				0.9:  0.01,  // PCT90
				0.95: 0.005, // PCT95
			},
			AgeBuckets: 1,
			MaxAge:     100000 * time.Second,
		},
		[]string{"method", "name"},
	)
)

// gauges for total
var (
	gaugeUsers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "users",
			Help: "The current number of users",
		},
	)
	gaugeState = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "state",
			Help: "The current runner state, 1=initializing, 2=spawning, 3=running, 4=stopping, 5=stopped, 6=quitting, 7=missing",
		},
	)
	gaugeDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "duration",
			Help: "The duration of load testing",
		},
	)
	gaugeTotalAverageResponseTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_average_response_time",
			Help: "The average response time in total milliseconds",
		},
	)
	gaugeTotalMinResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_min_response_time",
			Help: "The min response time in total milliseconds",
		},
		[]string{"method", "name"},
	)
	gaugeTotalMaxResponseTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_max_response_time",
			Help: "The max response time in total milliseconds",
		},
		[]string{"method", "name"},
	)
	gaugeTotalRPS = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_rps",
			Help: "The requests per second in total",
		},
	)
	gaugeTotalFailRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "fail_ratio",
			Help: "The ratio of request failures in total",
		},
	)
	gaugeTotalFailPerSec = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_fail_per_sec",
			Help: "The failure number per second in total",
		},
	)
	gaugeTransactionsPassed = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "transactions_passed",
			Help: "The accumulated number of passed transactions",
		},
	)
	gaugeTransactionsFailed = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "transactions_failed",
			Help: "The accumulated number of failed transactions",
		},
	)
)

var (
	minResponseTimeMap = sync.Map{}
	maxResponseTimeMap = sync.Map{}
)

// NewPrometheusPusherOutput returns a PrometheusPusherOutput.
func NewPrometheusPusherOutput(gatewayURL, jobName string, mode string) *PrometheusPusherOutput {
	return &PrometheusPusherOutput{
		pusher: push.New(gatewayURL, jobName).
			Grouping("instance", uuid.NewV1().String()).
			Grouping("mode", mode),
	}
}

func GetRegistry(isExporter bool) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		// gauge vectors for requests
		gaugeNumRequests,
		gaugeNumFailures,
		gaugeMedianResponseTime,
		gaugeAverageResponseTime,
		gaugeMinResponseTime,
		gaugeMaxResponseTime,
		gaugeAverageContentLength,
		gaugeCurrentRPS,
		gaugeCurrentFailPerSec,
		// counter for total
		counterTotalNumRequests,
		counterTotalNumFailures,
		// summary for total
		summaryResponseTime,
		// gauges for total
		gaugeUsers,
		gaugeState,
		gaugeDuration,
		gaugeTotalAverageResponseTime,
		gaugeTotalMinResponseTime,
		gaugeTotalMaxResponseTime,
		gaugeTotalRPS,
		gaugeTotalFailRatio,
		gaugeTotalFailPerSec,
		gaugeTransactionsPassed,
		gaugeTransactionsFailed,
	)
	if isExporter {
		registry.MustRegister(counterTotalErrors)
	} else {
		registry.MustRegister(counterErrors)
	}
	return registry
}

// PrometheusPusherOutput pushes boomer stats to Prometheus Pushgateway.
type PrometheusPusherOutput struct {
	pusher *push.Pusher // Prometheus Pushgateway Pusher
}

// OnStart will register all prometheus metric collectors
func (o *PrometheusPusherOutput) OnStart() {
	log.Info().Msg("register prometheus metric collectors")
	o.pusher = o.pusher.Gatherer(GetRegistry(false))
}

// OnStop of PrometheusPusherOutput has nothing to do.
func (o *PrometheusPusherOutput) OnStop() {
	// update runner state: stopped
	gaugeState.Set(float64(StateStopped))
	if err := o.pusher.Push(); err != nil {
		log.Error().Err(err).Msg("push to Pushgateway failed")
	}
}

// OnEvent will push metric to Prometheus Pushgataway
func (o *PrometheusPusherOutput) OnEvent(data map[string]interface{}) {
	output, err := convertData(data)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert data")
		return
	}

	setPrometheus(output, false)

	if err := o.pusher.Push(); err != nil {
		log.Error().Err(err).Msg("push to Pushgateway failed")
	}
}

func setPrometheus(output *dataOutput, isExporter bool) {
	// user count
	gaugeUsers.Set(float64(output.UserCount))

	// runner state
	gaugeState.Set(float64(output.State))

	// min/avg/max response time in total
	gaugeTotalAverageResponseTime.Set(output.TotalAvgResponseTime)
	gaugeTotalMinResponseTime.WithLabelValues("", "Total").Set(output.TotalMinResponseTime)
	gaugeTotalMaxResponseTime.WithLabelValues("", "Total").Set(output.TotalMaxResponseTime)

	// duration
	gaugeDuration.Set(output.Duration)

	// rps in total
	gaugeTotalRPS.Set(output.TotalRPS)

	// failure ratio in total
	gaugeTotalFailRatio.Set(output.TotalFailRatio)

	// failure per second in total
	gaugeTotalFailPerSec.Set(output.TotalFailPerSec)

	// accumulated number of transactions
	if isExporter {
		gaugeTransactionsPassed.Set(float64(output.TransactionsPassed))
		gaugeTransactionsFailed.Set(float64(output.TransactionsFailed))
	} else {
		gaugeTransactionsPassed.Add(float64(output.TransactionsPassed))
		gaugeTransactionsFailed.Add(float64(output.TransactionsFailed))
	}

	for _, stat := range output.Stats {
		method := stat.Method
		name := stat.Name
		// stats in stats interval
		gaugeNumRequests.WithLabelValues(method, name).Set(float64(stat.NumRequests))
		gaugeNumFailures.WithLabelValues(method, name).Set(float64(stat.NumFailures))
		gaugeMedianResponseTime.WithLabelValues(method, name).Set(float64(stat.medianResponseTime))
		gaugeAverageResponseTime.WithLabelValues(method, name).Set(float64(stat.avgResponseTime))
		gaugeMinResponseTime.WithLabelValues(method, name).Set(float64(stat.MinResponseTime))
		gaugeMaxResponseTime.WithLabelValues(method, name).Set(float64(stat.MaxResponseTime))
		gaugeAverageContentLength.WithLabelValues(method, name).Set(float64(stat.avgContentLength))
		gaugeCurrentRPS.WithLabelValues(method, name).Set(stat.currentRps)
		gaugeCurrentFailPerSec.WithLabelValues(method, name).Set(stat.currentFailPerSec)
		for responseTime, count := range stat.ResponseTimes {
			var i int64
			for i = 0; i < count; i++ {
				summaryResponseTime.WithLabelValues(method, name).Observe(float64(responseTime))
			}
		}
		// every stat in total
		key := fmt.Sprintf("%v_%v", method, name)
		minResponseTime, loaded := minResponseTimeMap.LoadOrStore(key, float64(stat.MinResponseTime))
		if loaded {
			minResponseTime = math.Min(minResponseTime.(float64), float64(stat.MinResponseTime))
			minResponseTimeMap.Store(key, minResponseTime)
		}
		gaugeTotalMinResponseTime.WithLabelValues(method, name).Set(minResponseTime.(float64))

		maxResponseTime, loaded := maxResponseTimeMap.LoadOrStore(key, float64(stat.MaxResponseTime))
		if loaded {
			maxResponseTime = math.Max(maxResponseTime.(float64), float64(stat.MaxResponseTime))
			maxResponseTimeMap.Store(key, maxResponseTime)
		}
		gaugeTotalMaxResponseTime.WithLabelValues(method, name).Set(maxResponseTime.(float64))

		if isExporter {
			counterTotalNumRequests.WithLabelValues(method, name).Set(float64(stat.NumRequests))
			counterTotalNumFailures.WithLabelValues(method, name).Set(float64(stat.NumFailures))
		} else {
			counterTotalNumRequests.WithLabelValues(method, name).Add(float64(stat.NumRequests))
			counterTotalNumFailures.WithLabelValues(method, name).Add(float64(stat.NumFailures))
		}
	}

	// errors
	for _, requestError := range output.Errors {
		if isExporter {
			counterTotalErrors.WithLabelValues(
				requestError["method"].(string),
				requestError["name"].(string),
				requestError["error"].(string),
			).Set(float64(requestError["occurrences"].(int64)))
		} else {
			counterErrors.WithLabelValues(
				requestError["method"].(string),
				requestError["name"].(string),
				requestError["error"].(string),
			).Add(float64(requestError["occurrences"].(int64)))
		}
	}
}

// resetPrometheusMetrics will reset all metrics
func resetPrometheusMetrics() {
	log.Info().Msg("reset all prometheus metrics")
	gaugeNumRequests.Reset()
	gaugeNumFailures.Reset()
	gaugeMedianResponseTime.Reset()
	gaugeAverageResponseTime.Reset()
	gaugeMinResponseTime.Reset()
	gaugeMaxResponseTime.Reset()
	gaugeAverageContentLength.Reset()
	gaugeCurrentRPS.Reset()
	gaugeCurrentFailPerSec.Reset()
	// counter for total
	counterErrors.Reset()
	counterTotalErrors.Reset()
	counterTotalNumRequests.Reset()
	counterTotalNumFailures.Reset()
	// summary for total
	summaryResponseTime.Reset()
	// gauges for total
	gaugeUsers.Set(0)
	gaugeState.Set(1)
	gaugeDuration.Set(0)
	gaugeTotalAverageResponseTime.Set(0)
	gaugeTotalMinResponseTime.Reset()
	gaugeTotalMaxResponseTime.Reset()
	gaugeTotalRPS.Set(0)
	gaugeTotalFailRatio.Set(0)
	gaugeTotalFailPerSec.Set(0)
	gaugeTransactionsPassed.Set(0)
	gaugeTransactionsFailed.Set(0)

	minResponseTimeMap = sync.Map{}
	maxResponseTimeMap = sync.Map{}
}
