package boomer

import (
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"

	"github.com/httprunner/httprunner/v4/hrp/internal/json"
)

// Mode is the running mode of boomer, both standalone and distributed are supported.
type Mode int

const (
	// DistributedMasterMode requires being connected by each worker.
	DistributedMasterMode Mode = iota
	// DistributedWorkerMode requires connecting to a master.
	DistributedWorkerMode
	// StandaloneMode will run without a master.
	StandaloneMode
)

// A Boomer is used to run tasks.
type Boomer struct {
	masterHost string
	masterPort int
	mode       Mode

	localRunner  *localRunner
	workerRunner *workerRunner
	masterRunner *masterRunner

	testcasePath []string

	cpuProfile         string
	cpuProfileDuration time.Duration

	memoryProfile         string
	memoryProfileDuration time.Duration

	disableKeepalive   bool
	disableCompression bool
}

type Profile struct {
	SpawnCount               int64         `json:"spawn-count,omitempty" yaml:"spawn-count,omitempty" mapstructure:"spawn-count,omitempty"`
	SpawnRate                float64       `json:"spawn-rate,omitempty" yaml:"spawn-rate,omitempty" mapstructure:"spawn-rate,omitempty"`
	RunTime                  int64         `json:"run-time,omitempty" yaml:"run-time,omitempty" mapstructure:"run-time,omitempty"`
	MaxRPS                   int64         `json:"max-rps,omitempty" yaml:"max-rps,omitempty" mapstructure:"max-rps,omitempty"`
	LoopCount                int64         `json:"loop-count,omitempty" yaml:"loop-count,omitempty" mapstructure:"loop-count,omitempty"`
	RequestIncreaseRate      string        `json:"request-increase-rate,omitempty" yaml:"request-increase-rate,omitempty" mapstructure:"request-increase-rate,omitempty"`
	MemoryProfile            string        `json:"memory-profile,omitempty" yaml:"memory-profile,omitempty" mapstructure:"memory-profile,omitempty"`
	MemoryProfileDuration    time.Duration `json:"memory-profile-duration,omitempty" yaml:"memory-profile-duration,omitempty" mapstructure:"memory-profile-duration,omitempty"`
	CPUProfile               string        `json:"cpu-profile,omitempty" yaml:"cpu-profile,omitempty" mapstructure:"cpu-profile,omitempty"`
	CPUProfileDuration       time.Duration `json:"cpu-profile-duration,omitempty" yaml:"cpu-profile-duration,omitempty" mapstructure:"cpu-profile-duration,omitempty"`
	PrometheusPushgatewayURL string        `json:"prometheus-gateway,omitempty" yaml:"prometheus-gateway,omitempty" mapstructure:"prometheus-gateway,omitempty"`
	PrometheusExporter       bool          `json:"prometheus-exporter,omitempty" yaml:"prometheus-exporter,omitempty" mapstructure:"prometheus-exporter,omitempty"`
	DisableConsoleOutput     bool          `json:"disable-console-output,omitempty" yaml:"disable-console-output,omitempty" mapstructure:"disable-console-output,omitempty"`
	DisableCompression       bool          `json:"disable-compression,omitempty" yaml:"disable-compression,omitempty" mapstructure:"disable-compression,omitempty"`
	DisableKeepalive         bool          `json:"disable-keepalive,omitempty" yaml:"disable-keepalive,omitempty" mapstructure:"disable-keepalive,omitempty"`
}

func NewProfile() *Profile {
	return &Profile{
		SpawnCount:            1,
		SpawnRate:             1,
		MaxRPS:                -1,
		LoopCount:             -1,
		RequestIncreaseRate:   "-1",
		CPUProfileDuration:    30 * time.Second,
		MemoryProfileDuration: 30 * time.Second,
	}
}

func (b *Boomer) GetProfile() *Profile {
	switch b.mode {
	case DistributedMasterMode:
		return b.masterRunner.profile
	case DistributedWorkerMode:
		return b.workerRunner.profile
	default:
		return b.localRunner.profile
	}
}

func (b *Boomer) SetProfile(profile *Profile) {
	switch b.mode {
	case DistributedMasterMode:
		b.masterRunner.profile = profile
	case DistributedWorkerMode:
		b.workerRunner.profile = profile
	default:
		b.localRunner.profile = profile
	}
}

// SetMode only accepts boomer.DistributedMasterMode、boomer.DistributedWorkerMode and boomer.StandaloneMode.
func (b *Boomer) SetMode(mode Mode) {
	switch mode {
	case DistributedMasterMode:
		b.mode = DistributedMasterMode
	case DistributedWorkerMode:
		b.mode = DistributedWorkerMode
	case StandaloneMode:
		b.mode = StandaloneMode
	default:
		log.Error().Err(errors.New("Invalid mode, ignored!"))
	}
}

// GetMode returns boomer operating mode
func (b *Boomer) GetMode() string {
	switch b.mode {
	case DistributedMasterMode:
		return "master"
	case DistributedWorkerMode:
		return "worker"
	case StandaloneMode:
		return "standalone"
	default:
		log.Error().Err(errors.New("Invalid mode, ignored!"))
		return ""
	}
}

// NewStandaloneBoomer returns a new Boomer, which can run without master.
func NewStandaloneBoomer(spawnCount int64, spawnRate float64) *Boomer {
	return &Boomer{
		mode:        StandaloneMode,
		localRunner: newLocalRunner(spawnCount, spawnRate),
	}
}

// NewMasterBoomer returns a new Boomer.
func NewMasterBoomer(masterBindHost string, masterBindPort int) *Boomer {
	return &Boomer{
		masterRunner: newMasterRunner(masterBindHost, masterBindPort),
		mode:         DistributedMasterMode,
	}
}

// NewWorkerBoomer returns a new Boomer.
func NewWorkerBoomer(masterHost string, masterPort int) *Boomer {
	return &Boomer{
		workerRunner: newWorkerRunner(masterHost, masterPort),
		masterHost:   masterHost,
		masterPort:   masterPort,
		mode:         DistributedWorkerMode,
	}
}

// SetAutoStart auto start to load testing
func (b *Boomer) SetAutoStart() {
	b.masterRunner.autoStart = true
}

// RunMaster start to run master runner
func (b *Boomer) RunMaster() {
	b.masterRunner.run()
}

// RunWorker start to run worker runner
func (b *Boomer) RunWorker() {
	b.workerRunner.run()
}

// TestCaseBytesChan gets test case bytes chan
func (b *Boomer) TestCaseBytesChan() chan []byte {
	return b.masterRunner.testCaseBytesChan
}

func (b *Boomer) GetTestCaseBytes() []byte {
	switch b.mode {
	case DistributedMasterMode:
		return b.masterRunner.testCasesBytes
	case DistributedWorkerMode:
		return b.workerRunner.testCasesBytes
	default:
		return nil
	}
}

func ProfileToBytes(profile *Profile) []byte {
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal testcases")
		return nil
	}
	return profileBytes
}

func BytesToProfile(profileBytes []byte) *Profile {
	var profile *Profile
	err := json.Unmarshal(profileBytes, &profile)
	if err != nil {
		log.Error().Err(err).Msg("failed to unmarshal testcases")
	}
	return profile
}

// GetTasksChan getsTasks chan
func (b *Boomer) GetTasksChan() chan *task {
	switch b.mode {
	case DistributedWorkerMode:
		return b.workerRunner.tasksChan
	default:
		return nil
	}
}

func (b *Boomer) GetRebalanceChan() chan bool {
	switch b.mode {
	case DistributedWorkerMode:
		return b.workerRunner.controller.getRebalanceChan()
	default:
		return nil
	}
}

func (b *Boomer) SetTestCasesPath(paths []string) {
	b.testcasePath = paths
}

func (b *Boomer) GetTestCasesPath() []string {
	return b.testcasePath
}

func (b *Boomer) ParseTestCasesChan() chan bool {
	return b.masterRunner.parseTestCasesChan
}

// GetMasterHost returns master IP
func (b *Boomer) GetMasterHost() string {
	return b.masterHost
}

// GetState gets worker state
func (b *Boomer) GetState() int32 {
	switch b.mode {
	case DistributedWorkerMode:
		return b.workerRunner.getState()
	case DistributedMasterMode:
		return b.masterRunner.getState()
	default:
		return b.localRunner.getState()
	}
}

// SetSpawnCount sets spawn count
func (b *Boomer) SetSpawnCount(spawnCount int64) {
	switch b.mode {
	case DistributedMasterMode:
		b.masterRunner.setSpawnCount(spawnCount)
	case DistributedWorkerMode:
		b.workerRunner.setSpawnCount(spawnCount)
	default:
		b.localRunner.setSpawnCount(spawnCount)
	}
}

// SetSpawnRate sets spawn rate
func (b *Boomer) SetSpawnRate(spawnRate float64) {
	switch b.mode {
	case DistributedMasterMode:
		b.masterRunner.setSpawnRate(spawnRate)
	case DistributedWorkerMode:
		b.workerRunner.setSpawnRate(spawnRate)
	default:
		b.localRunner.setSpawnRate(spawnRate)
	}
}

// SetRunTime sets run time
func (b *Boomer) SetRunTime(runTime int64) {
	switch b.mode {
	case DistributedMasterMode:
		b.masterRunner.setRunTime(runTime)
	case DistributedWorkerMode:
		b.workerRunner.setRunTime(runTime)
	default:
		b.localRunner.setRunTime(runTime)
	}
}

// SetExpectWorkers sets expect workers while load testing
func (b *Boomer) SetExpectWorkers(expectWorkers int, expectWorkersMaxWait int) {
	b.masterRunner.setExpectWorkers(expectWorkers, expectWorkersMaxWait)
}

// SetRateLimiter creates rate limiter with the given limit and burst.
func (b *Boomer) SetRateLimiter(maxRPS int64, requestIncreaseRate string) {
	var rateLimiter RateLimiter
	var err error
	if requestIncreaseRate != "-1" {
		if maxRPS <= 0 {
			maxRPS = math.MaxInt64
		}
		log.Warn().Int64("maxRPS", maxRPS).Str("increaseRate", requestIncreaseRate).Msg("set ramp up rate limiter")
		rateLimiter, err = NewRampUpRateLimiter(maxRPS, requestIncreaseRate, time.Second)
	} else {
		if maxRPS > 0 {
			log.Warn().Int64("maxRPS", maxRPS).Msg("set stable rate limiter")
			rateLimiter = NewStableRateLimiter(maxRPS, time.Second)
		}
	}
	if err != nil {
		log.Error().Err(err).Msg("failed to create rate limiter")
		return
	}

	if rateLimiter != nil {
		switch b.mode {
		case DistributedWorkerMode:
			b.workerRunner.rateLimitEnabled = true
			b.workerRunner.rateLimiter = rateLimiter
		case StandaloneMode:
			b.localRunner.rateLimitEnabled = true
			b.localRunner.rateLimiter = rateLimiter
		}
	}
}

// SetDisableKeepAlive disable keep-alive for tcp
func (b *Boomer) SetDisableKeepAlive(disableKeepalive bool) {
	b.disableKeepalive = disableKeepalive
}

// SetIgnoreQuit not quit while master quit
func (b *Boomer) SetIgnoreQuit() {
	b.workerRunner.ignoreQuit = true
}

// SetDisableCompression disable compression to prevent the Transport from requesting compression with an "Accept-Encoding: gzip"
func (b *Boomer) SetDisableCompression(disableCompression bool) {
	b.disableCompression = disableCompression
}

func (b *Boomer) GetDisableKeepAlive() bool {
	return b.disableKeepalive
}

func (b *Boomer) GetDisableCompression() bool {
	return b.disableCompression
}

// SetLoopCount set loop count for test.
func (b *Boomer) SetLoopCount(loopCount int64) {
	// total loop count for testcase, it will be evenly distributed to each worker
	switch b.mode {
	case DistributedWorkerMode:
		b.workerRunner.loop = &Loop{loopCount: loopCount * b.workerRunner.getSpawnCount()}
	case DistributedMasterMode:
		b.masterRunner.loop = &Loop{loopCount: loopCount * b.masterRunner.getSpawnCount()}
	case StandaloneMode:
		b.localRunner.loop = &Loop{loopCount: loopCount * b.localRunner.getSpawnCount()}
	}
}

// AddOutput accepts outputs which implements the boomer.Output interface.
func (b *Boomer) AddOutput(o Output) {
	switch b.mode {
	case DistributedWorkerMode:
		b.workerRunner.addOutput(o)
	case DistributedMasterMode:
		b.masterRunner.addOutput(o)
	case StandaloneMode:
		b.localRunner.addOutput(o)
	}
}

// EnableCPUProfile will start cpu profiling after run.
func (b *Boomer) EnableCPUProfile(cpuProfile string, duration time.Duration) {
	b.cpuProfile = cpuProfile
	b.cpuProfileDuration = duration
}

// EnableMemoryProfile will start memory profiling after run.
func (b *Boomer) EnableMemoryProfile(memoryProfile string, duration time.Duration) {
	b.memoryProfile = memoryProfile
	b.memoryProfileDuration = duration
}

// EnableGracefulQuit catch SIGINT and SIGTERM signals to quit gracefully
func (b *Boomer) EnableGracefulQuit(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-c
		b.Quit()
		cancel()
	}()
	return ctx
}

// Run accepts a slice of Task and connects to the locust master.
func (b *Boomer) Run(tasks ...*Task) {
	if b.cpuProfile != "" {
		err := startCPUProfile(b.cpuProfile, b.cpuProfileDuration)
		if err != nil {
			log.Error().Err(err).Msg("failed to start cpu profiling")
		}
	}
	if b.memoryProfile != "" {
		err := startMemoryProfile(b.memoryProfile, b.memoryProfileDuration)
		if err != nil {
			log.Error().Err(err).Msg("failed to start memory profiling")
		}
	}

	switch b.mode {
	case DistributedWorkerMode:
		log.Info().Msg("running in worker mode")
		b.workerRunner.setTasks(tasks)
		b.workerRunner.start()
	case StandaloneMode:
		log.Info().Msg("running in standalone mode")
		b.localRunner.setTasks(tasks)
		b.localRunner.start()
	default:
		log.Error().Err(errors.New("Invalid mode, expected boomer.DistributedMode or boomer.StandaloneMode"))
	}
}

func (b *Boomer) SetTasks(tasks ...*Task) {
	switch b.mode {
	case DistributedWorkerMode:
		log.Info().Msg("set tasks to worker")
		b.workerRunner.setTasks(tasks)
	case StandaloneMode:
		log.Info().Msg("set tasks to standalone")
		b.localRunner.setTasks(tasks)
	default:
		log.Error().Err(errors.New("Invalid mode, expected boomer.DistributedMode or boomer.StandaloneMode"))
	}
}

// RecordTransaction reports a transaction stat.
func (b *Boomer) RecordTransaction(name string, success bool, elapsedTime int64, contentSize int64) {
	var runnerStats *requestStats
	switch b.mode {
	case DistributedWorkerMode:
		runnerStats = b.workerRunner.stats
	case DistributedMasterMode:
		runnerStats = b.masterRunner.stats
	case StandaloneMode:
		runnerStats = b.localRunner.stats
	}
	runnerStats.transactionChan <- &transaction{
		name:        name,
		success:     success,
		elapsedTime: elapsedTime,
		contentSize: contentSize,
	}
}

// RecordSuccess reports a success.
func (b *Boomer) RecordSuccess(requestType, name string, responseTime int64, responseLength int64) {
	var runnerStats *requestStats
	switch b.mode {
	case DistributedWorkerMode:
		runnerStats = b.workerRunner.stats
	case DistributedMasterMode:
		runnerStats = b.masterRunner.stats
	case StandaloneMode:
		runnerStats = b.localRunner.stats
	}
	runnerStats.requestSuccessChan <- &requestSuccess{
		requestType:    requestType,
		name:           name,
		responseTime:   responseTime,
		responseLength: responseLength,
	}
}

// RecordFailure reports a failure.
func (b *Boomer) RecordFailure(requestType, name string, responseTime int64, exception string) {
	var runnerStats *requestStats
	switch b.mode {
	case DistributedWorkerMode:
		runnerStats = b.workerRunner.stats
	case DistributedMasterMode:
		runnerStats = b.masterRunner.stats
	case StandaloneMode:
		runnerStats = b.localRunner.stats
	}
	runnerStats.requestFailureChan <- &requestFailure{
		requestType:  requestType,
		name:         name,
		responseTime: responseTime,
		errMsg:       exception,
	}
}

// Start starts to run
func (b *Boomer) Start(Args *Profile) error {
	if b.masterRunner.isStarting() {
		return errors.New("already started")
	}
	if b.masterRunner.isStopping() {
		return errors.New("Please wait for all workers to finish")
	}
	if int(Args.SpawnCount) < b.masterRunner.server.getAvailableClientsLength() {
		return errors.New("spawn count should be greater than available worker count")
	}
	b.SetSpawnCount(Args.SpawnCount)
	b.SetSpawnRate(Args.SpawnRate)
	b.SetRunTime(Args.RunTime)
	b.SetProfile(Args)
	err := b.masterRunner.start()
	return err
}

// ReBalance starts to rebalance load test
func (b *Boomer) ReBalance(Args *Profile) error {
	if !b.masterRunner.isStarting() {
		return errors.New("no start")
	}
	if int(Args.SpawnCount) < b.masterRunner.server.getAvailableClientsLength() {
		return errors.New("spawn count should be greater than available worker count")
	}
	b.SetSpawnCount(Args.SpawnCount)
	b.SetSpawnRate(Args.SpawnRate)
	b.SetRunTime(Args.RunTime)
	b.SetProfile(Args)
	err := b.masterRunner.rebalance()
	if err != nil {
		log.Error().Err(err).Msg("failed to rebalance")
	}
	return err
}

// Stop stops to load test
func (b *Boomer) Stop() error {
	return b.masterRunner.stop()
}

// GetWorkersInfo gets workers information
func (b *Boomer) GetWorkersInfo() []WorkerNode {
	return b.masterRunner.server.getAllWorkers()
}

// GetMasterInfo gets master information
func (b *Boomer) GetMasterInfo() map[string]interface{} {
	masterInfo := make(map[string]interface{})
	masterInfo["state"] = b.masterRunner.getState()
	masterInfo["workers"] = b.masterRunner.server.getAvailableClientsLength()
	masterInfo["target_users"] = b.masterRunner.getSpawnCount()
	masterInfo["current_users"] = b.masterRunner.server.getCurrentUsers()
	return masterInfo
}

func (b *Boomer) GetCloseChan() chan bool {
	switch b.mode {
	case DistributedWorkerMode:
		return b.workerRunner.closeChan
	case DistributedMasterMode:
		return b.masterRunner.closeChan
	default:
		return b.localRunner.closeChan
	}
}

// Quit will send a quit message to the master.
func (b *Boomer) Quit() {
	switch b.mode {
	case DistributedWorkerMode:
		b.workerRunner.stop()
		b.workerRunner.close()
	case DistributedMasterMode:
		b.masterRunner.close()
	case StandaloneMode:
		b.localRunner.stop()
	}
}

func (b *Boomer) GetSpawnDoneChan() chan struct{} {
	switch b.mode {
	case DistributedWorkerMode:
		return b.workerRunner.controller.getSpawnDone()
	case DistributedMasterMode:
		return b.masterRunner.controller.getSpawnDone()
	default:
		return b.localRunner.controller.getSpawnDone()
	}
}

func (b *Boomer) GetSpawnCount() int {
	switch b.mode {
	case DistributedWorkerMode:
		return int(b.workerRunner.getSpawnCount())
	case DistributedMasterMode:
		return int(b.masterRunner.getSpawnCount())
	default:
		return int(b.localRunner.getSpawnCount())
	}
}

func (b *Boomer) ResetStartTime() {
	switch b.mode {
	case DistributedWorkerMode:
		b.workerRunner.stats.total.resetStartTime()
	case DistributedMasterMode:
		b.masterRunner.stats.total.resetStartTime()
	default:
		b.localRunner.stats.total.resetStartTime()
	}
}

func (b *Boomer) SetExporterMode(open bool) {
	switch b.mode {
	case DistributedWorkerMode:
		b.workerRunner.exporterMode = open
	default:
	}
}

func (b *Boomer) SetPrometheusMetrics() {
	output := b.masterRunner.getDataOutput()
	userCount := int64(0)
	workers := b.GetWorkersInfo()
	for i := range workers {
		worker := &workers[i] // 使用临时变量保存每次迭代的值的指针
		worker.mutex.RLock()
		count := worker.getUserCount()
		worker.mutex.RUnlock()
		userCount += count
	}
	output.UserCount = userCount
	setPrometheus(output, true)

}
