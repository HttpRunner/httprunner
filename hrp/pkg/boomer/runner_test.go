package boomer

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/httprunner/httprunner/v4/hrp/internal/builtin"
	"github.com/httprunner/httprunner/v4/hrp/pkg/boomer/grpc/messager"
)

type HitOutput struct {
	onStart bool
	onEvent bool
	onStop  bool
}

func (o *HitOutput) OnStart() {
	o.onStart = true
}

func (o *HitOutput) OnEvent(data map[string]interface{}) {
	o.onEvent = true
}

func (o *HitOutput) OnStop() {
	o.onStop = true
}

func TestSafeRun(t *testing.T) {
	runner := &runner{}
	runner.safeRun(func() {
		panic("Runner will catch this panic")
	})
}

func TestOutputOnStart(t *testing.T) {
	hitOutput := &HitOutput{}
	hitOutput2 := &HitOutput{}
	runner := &runner{}
	runner.addOutput(hitOutput)
	runner.addOutput(hitOutput2)
	runner.outputOnStart()
	if !hitOutput.onStart {
		t.Error("hitOutput's OnStart has not been called")
	}
	if !hitOutput2.onStart {
		t.Error("hitOutput2's OnStart has not been called")
	}
}

func TestOutputOnEvent(t *testing.T) {
	hitOutput := &HitOutput{}
	hitOutput2 := &HitOutput{}
	runner := &runner{}
	runner.addOutput(hitOutput)
	runner.addOutput(hitOutput2)
	runner.outputOnEvent(nil)
	if !hitOutput.onEvent {
		t.Error("hitOutput's OnEvent has not been called")
	}
	if !hitOutput2.onEvent {
		t.Error("hitOutput2's OnEvent has not been called")
	}
}

func TestOutputOnStop(t *testing.T) {
	hitOutput := &HitOutput{}
	hitOutput2 := &HitOutput{}
	runner := &runner{}
	runner.addOutput(hitOutput)
	runner.addOutput(hitOutput2)
	runner.outputOnStop()
	if !hitOutput.onStop {
		t.Error("hitOutput's OnStop has not been called")
	}
	if !hitOutput2.onStop {
		t.Error("hitOutput2's OnStop has not been called")
	}
}

func TestLocalRunner(t *testing.T) {
	taskA := &Task{
		Weight: 10,
		Fn: func() {
			time.Sleep(time.Second)
		},
		Name: "TaskA",
	}
	tasks := []*Task{taskA}
	runner := newLocalRunner(2, 2)
	runner.setTasks(tasks)
	go runner.start()
	time.Sleep(4 * time.Second)
	runner.stop()
}

func TestLoopCount(t *testing.T) {
	taskA := &Task{
		Weight: 10,
		Fn: func() {
			time.Sleep(time.Millisecond)
		},
		Name: "TaskA",
	}
	tasks := []*Task{taskA}
	runner := newLocalRunner(2, 2)
	runner.loop = &Loop{loopCount: 4}
	runner.setTasks(tasks)
	runner.start()
	if !assert.Equal(t, atomic.LoadInt64(&runner.loop.loopCount), atomic.LoadInt64(&runner.loop.finishedCount)) {
		t.Fatal()
	}
}

func TestStopNotify(t *testing.T) {
	r := &localRunner{
		runner: runner{
			stopChan: make(chan bool),
			doneChan: make(chan bool),
		},
	}
	go func() {
		<-r.stopChan
		close(r.doneChan)
	}()

	notifier := r.stopNotify()
	select {
	case <-notifier:
		t.Fatalf("received unexpected stop notification")
	default:
	}
	r.gracefulStop()
	select {
	case <-notifier:
	default:
		t.Fatalf("cannot receive stop notification")
	}
}

func TestSpawnWorkers(t *testing.T) {
	taskA := &Task{
		Weight: 10,
		Fn: func() {
			time.Sleep(time.Second)
		},
		Name: "TaskA",
	}
	tasks := []*Task{taskA}

	runner := newWorkerRunner("localhost", 5557)
	defer runner.close()

	runner.client = newClient("localhost", 5557, runner.nodeID)
	runner.reset()
	runner.setTasks(tasks)
	go runner.spawnWorkers(10, 10, runner.stopChan, runner.spawnComplete)
	time.Sleep(2 * time.Second)

	currentClients := runner.controller.getCurrentClientsNum()
	if currentClients != 10 {
		t.Error("Unexpected count", currentClients)
	}
}

func TestSpawnWorkersWithManyTasks(t *testing.T) {
	var lock sync.Mutex
	taskCalls := map[string]int{}

	createTask := func(name string, weight int) *Task {
		return &Task{
			Name:   name,
			Weight: weight,
			Fn: func() {
				lock.Lock()
				taskCalls[name]++
				lock.Unlock()
			},
		}
	}
	tasks := []*Task{
		createTask("one hundred", 100),
		createTask("ten", 10),
		createTask("one", 1),
	}

	runner := newWorkerRunner("localhost", 5557)
	defer runner.close()

	runner.reset()
	runner.setTasks(tasks)
	runner.client = newClient("localhost", 5557, runner.nodeID)

	const numToSpawn int64 = 20

	go runner.spawnWorkers(numToSpawn, float64(numToSpawn), runner.stopChan, runner.spawnComplete)
	time.Sleep(5 * time.Second)

	currentClients := runner.controller.getCurrentClientsNum()

	assert.Equal(t, numToSpawn, int64(currentClients))
	lock.Lock()
	hundreds := taskCalls["one hundred"]
	tens := taskCalls["ten"]
	ones := taskCalls["one"]
	lock.Unlock()

	total := hundreds + tens + ones
	t.Logf("total tasks: %d, hundreds: %d, tens: %d, ones: %d\n",
		total, hundreds, tens, ones)

	assert.True(t, total > 111)

	assert.True(t, ones > 1)
	actPercentage := float64(ones) / float64(total)
	expectedPercentage := 1.0 / 111.0
	if actPercentage > 4*expectedPercentage || actPercentage < 0.25*expectedPercentage {
		t.Errorf("Unexpected percentage of ones task: exp %v, act %v", expectedPercentage, actPercentage)
	}

	assert.True(t, tens > 10)
	actPercentage = float64(tens) / float64(total)
	expectedPercentage = 10.0 / 111.0
	if actPercentage > 4*expectedPercentage || actPercentage < 0.25*expectedPercentage {
		t.Errorf("Unexpected percentage of tens task: exp %v, act %v", expectedPercentage, actPercentage)
	}

	assert.True(t, hundreds > 100)
	actPercentage = float64(hundreds) / float64(total)
	expectedPercentage = 100.0 / 111.0
	if actPercentage > 1 || actPercentage < 0.25*expectedPercentage {
		t.Errorf("Unexpected percentage of hundreds task: exp %v, act %v", expectedPercentage, actPercentage)
	}
}

func TestSpawnAndStop(t *testing.T) {
	taskA := &Task{
		Fn: func() {
			time.Sleep(time.Second)
		},
	}
	taskB := &Task{
		Fn: func() {
			time.Sleep(2 * time.Second)
		},
	}
	tasks := []*Task{taskA, taskB}
	runner := newWorkerRunner("localhost", 5557)
	defer runner.close()
	runner.client = newClient("localhost", 5557, runner.nodeID)

	runner.setTasks(tasks)
	runner.setSpawnCount(10)
	runner.setSpawnRate(10)

	go runner.start()

	// wait for spawning goroutines
	time.Sleep(5 * time.Second)
	if runner.controller.getCurrentClientsNum() != 10 {
		t.Error("Number of goroutines mismatches, expected: 10, current count", runner.controller.getCurrentClientsNum())
	}

	msg := <-runner.client.sendChannel()
	if msg.Type != "spawning_complete" {
		t.Error("Runner should send spawning_complete message when spawning completed, got", msg.Type)
	}
	go runner.stop()

	runner.onQuiting()
	msg = <-runner.client.sendChannel()
	if msg.Type != "quit" {
		t.Error("Runner should send quit message on quitting, got", msg.Type)
	}
}

func TestStop(t *testing.T) {
	taskA := &Task{
		Fn: func() {
			time.Sleep(time.Second)
		},
	}
	tasks := []*Task{taskA}
	runner := newWorkerRunner("localhost", 5557)
	runner.setTasks(tasks)
	runner.reset()
	runner.updateState(StateSpawning)

	go runner.stop()
	close(runner.doneChan)
	time.Sleep(1 * time.Second)
	if runner.getState() != StateStopped {
		t.Error("Expected runner state to be 5, was", getStateName(runner.getState()))
	}
}

func TestOnSpawnMessage(t *testing.T) {
	taskA := &Task{
		Fn: func() {
			time.Sleep(time.Second)
		},
	}
	runner := newWorkerRunner("localhost", 5557)
	defer runner.close()
	runner.client = newClient("localhost", 5557, runner.nodeID)
	runner.updateState(StateInit)
	runner.reset()
	runner.setTasks([]*Task{taskA})
	runner.setSpawnCount(100)
	runner.setSpawnRate(100)
	runner.onSpawnMessage(newMessageToWorker("spawn", ProfileToBytes(&Profile{SpawnCount: 20, SpawnRate: 20}), nil, nil))

	if runner.getSpawnCount() != 20 {
		t.Error("workers should be overwrote by onSpawnMessage, expected: 20, was:", runner.controller.spawnCount)
	}
	if runner.getSpawnRate() != 20 {
		t.Error("spawnRate should be overwrote by onSpawnMessage, expected: 20, was:", runner.controller.spawnRate)
	}

	runner.onMessage(newGenericMessage("stop", nil, runner.nodeID))
}

func TestOnQuitMessage(t *testing.T) {
	runner := newWorkerRunner("localhost", 5557)
	runner.client = newClient("localhost", 5557, "test")
	runner.updateState(StateInit)

	runner.onMessage(newGenericMessage("quit", nil, runner.nodeID))
	<-runner.closeChan

	runner.updateState(StateRunning)
	runner.reset()
	runner.closeChan = make(chan bool)
	runner.client.shutdownChan = make(chan bool)
	go runner.onMessage(newGenericMessage("quit", nil, runner.nodeID))
	close(runner.doneChan)
	<-runner.closeChan
	runner.onQuiting()
	if runner.getState() != StateQuitting {
		t.Error("Runner's state should be StateQuitting")
	}

	runner.updateState(StateStopped)
	runner.closeChan = make(chan bool)
	runner.reset()
	runner.client.shutdownChan = make(chan bool)
	runner.onMessage(newGenericMessage("quit", nil, runner.nodeID))
	<-runner.closeChan
	runner.onQuiting()
	if runner.getState() != StateQuitting {
		t.Error("Runner's state should be StateQuitting")
	}
}

func TestOnMessage(t *testing.T) {
	taskA := &Task{
		Fn: func() {
			time.Sleep(time.Second)
		},
	}
	taskB := &Task{
		Fn: func() {
			time.Sleep(2 * time.Second)
		},
	}
	tasks := []*Task{taskA, taskB}

	runner := newWorkerRunner("localhost", 5557)
	runner.client = newClient("localhost", 5557, runner.nodeID)
	runner.updateState(StateInit)
	runner.setTasks(tasks)

	// start spawning
	runner.onMessage(newMessageToWorker("spawn", ProfileToBytes(&Profile{SpawnCount: 10, SpawnRate: 10}), nil, nil))
	go runner.start()

	msg := <-runner.client.sendChannel()
	if msg.Type != "spawning" {
		t.Error("Runner should send spawning message when starting spawn, got", msg.Type)
	}

	// spawn complete and running
	time.Sleep(5 * time.Second)
	if runner.controller.getCurrentClientsNum() != 10 {
		t.Error("Number of goroutines mismatches, expected: 10, current count:", runner.controller.getCurrentClientsNum())
	}
	msg = <-runner.client.sendChannel()
	if msg.Type != "spawning_complete" {
		t.Error("Runner should send spawning_complete message when spawn completed, got", msg.Type)
	}
	if runner.getState() != StateRunning {
		t.Error("State of runner is not running after spawn, got", getStateName(runner.getState()))
	}

	// increase goroutines while running
	runner.onMessage(newMessageToWorker("rebalance", ProfileToBytes(&Profile{SpawnCount: 15, SpawnRate: 15}), nil, nil))
	runner.controller.rebalance <- true

	time.Sleep(2 * time.Second)
	if runner.getState() != StateRunning {
		t.Error("State of runner is not running after spawn, got", getStateName(runner.getState()))
	}
	if runner.controller.getCurrentClientsNum() != 15 {
		t.Error("Number of goroutines mismatches, expected: 15, current count:", runner.controller.getCurrentClientsNum())
	}

	// stop all the workers
	runner.onMessage(newGenericMessage("stop", nil, runner.nodeID))
	if runner.getState() != StateStopped {
		t.Error("State of runner is not stopped, got", getStateName(runner.getState()))
	}
	msg = <-runner.client.sendChannel()
	if msg.Type != "client_stopped" {
		t.Error("Runner should send client_stopped message, got", msg.Type)
	}

	time.Sleep(3 * time.Second)

	// spawn again
	runner.onMessage(newMessageToWorker("spawn", ProfileToBytes(&Profile{SpawnCount: 10, SpawnRate: 10}), nil, nil))
	go runner.start()

	msg = <-runner.client.sendChannel()
	if msg.Type != "spawning" {
		t.Error("Runner should send spawning message when starting spawn, got", msg.Type)
	}

	// spawn complete and running
	time.Sleep(5 * time.Second)
	if runner.controller.getCurrentClientsNum() != 10 {
		t.Error("Number of goroutines mismatches, expected: 10, current count:", runner.controller.getCurrentClientsNum())
	}
	if runner.getState() != StateRunning {
		t.Error("State of runner is not running after spawn, got", getStateName(runner.getState()))
	}
	msg = <-runner.client.sendChannel()
	if msg.Type != "spawning_complete" {
		t.Error("Runner should send spawning_complete message when spawn completed, got", msg.Type)
	}

	// stop all the workers
	runner.onMessage(newGenericMessage("stop", nil, runner.nodeID))
	if runner.getState() != StateStopped {
		t.Error("State of runner is not stopped, got", getStateName(runner.getState()))
	}
	msg = <-runner.client.sendChannel()
	if msg.Type != "client_stopped" {
		t.Error("Runner should send client_stopped message, got", msg.Type)
	}

	time.Sleep(3 * time.Second)
	// quit
	runner.onMessage(newGenericMessage("quit", nil, runner.nodeID))
}

func TestClientListener(t *testing.T) {
	runner := newMasterRunner("localhost", 5557)
	defer runner.close()
	runner.updateState(StateInit)
	runner.setSpawnCount(10)
	runner.setSpawnRate(10)
	go runner.stateMachine()
	go runner.clientListener()
	runner.server.clients.Store("testID1", &WorkerNode{ID: "testID1", Heartbeat: 3, stream: make(chan *messager.StreamResponse, 10)})
	runner.server.clients.Store("testID2", &WorkerNode{ID: "testID2", Heartbeat: 3, stream: make(chan *messager.StreamResponse, 10)})
	runner.server.recvChannel() <- &genericMessage{
		Type:   typeClientReady,
		NodeID: "testID1",
	}
	worker1, ok := runner.server.getClients().Load("testID1")
	if !ok {
		t.Fatal("error")
	}
	workerInfo1, ok := worker1.(*WorkerNode)
	if !ok {
		t.Fatal("error")
	}
	time.Sleep(time.Second)
	if workerInfo1.getState() != StateInit {
		t.Error("State of worker runner is not init, got", workerInfo1.getState())
	}
	runner.server.recvChannel() <- &genericMessage{
		Type:   typeClientStopped,
		NodeID: "testID2",
	}
	runner.updateState(StateRunning)
	worker2, ok := runner.server.getClients().Load("testID2")
	if !ok {
		t.Fatal("error")
	}
	workerInfo2, ok := worker2.(*WorkerNode)
	if !ok {
		t.Fatal("error")
	}
	time.Sleep(time.Second)
	if workerInfo2.getState() != StateStopped {
		t.Error("State of worker runner is not stopped, got", workerInfo2.getState())
	}
	runner.server.recvChannel() <- &genericMessage{
		Type:   typeClientStopped,
		NodeID: "testID1",
	}
	time.Sleep(time.Second)
	if runner.getState() != StateStopped {
		t.Error("State of master runner is not stopped, got", getStateName(runner.getState()))
	}
}

func TestHeartbeatWorker(t *testing.T) {
	runner := newMasterRunner("localhost", 5557)
	defer runner.close()
	runner.updateState(StateInit)
	runner.setSpawnCount(10)
	runner.setSpawnRate(10)
	runner.server.clients.Store("testID1", &WorkerNode{ID: "testID1", Heartbeat: 1, State: StateInit, stream: make(chan *messager.StreamResponse, 10)})
	runner.server.clients.Store("testID2", &WorkerNode{ID: "testID2", Heartbeat: 1, State: StateInit, stream: make(chan *messager.StreamResponse, 10)})
	go runner.clientListener()
	go runner.heartbeatWorker()
	time.Sleep(4 * time.Second)
	worker1, ok := runner.server.getClients().Load("testID1")
	if !ok {
		t.Fatal()
	}
	workerInfo1, ok := worker1.(*WorkerNode)
	if !ok {
		t.Fatal()
	}
	if workerInfo1.getState() != StateMissing {
		t.Error("expected state of worker runner is missing, but got", getStateName(workerInfo1.getState()))
	}
	runner.server.recvChannel() <- &genericMessage{
		Type:   typeHeartbeat,
		NodeID: "testID2",
		Data:   map[string][]byte{"state": builtin.Int64ToBytes(3)},
	}
	worker2, ok := runner.server.getClients().Load("testID2")
	if !ok {
		t.Fatal()
	}
	workerInfo2, ok := worker2.(*WorkerNode)
	if !ok {
		t.Fatal()
	}
	time.Sleep(time.Second)
	if workerInfo2.getState() == StateMissing {
		t.Error("expected state of worker runner is not missing, but got missing")
	}
}

func TestHandleStat(t *testing.T) {
	runner := newMasterRunner("localhost", 5557)
	var d = make(map[string][]byte)
	data := map[string]interface{}{
		"state": 1,
		"transactions": map[string]int64{
			"passed": 10,
			"failed": 2,
		},
		"stats": []interface{}{
			map[string]interface{}{
				"last_request_timestamp": 1723188340177, "max_response_time": 2703, "method": "request-GET",
				"min_response_time": 2703, "name": "HTTP/2 get", "num_failures": 0, "num_none_requests": 0, "num_requests": 1,
				"start_time": 0, "total_content_length": 522, "total_response_time": 2703,
			},
			map[string]interface{}{"last_request_timestamp": 1723188340452, "max_response_time": 43873, "method": "transaction",
				"min_response_time": 43873, "name": "Action", "num_failures": 0, "num_none_requests": 0, "num_requests": 1,
				"start_time": 0, "total_content_length": 0, "total_response_time": 43873},
		},
		"stats_total": map[string]interface{}{
			"last_request_timestamp": 1723188340452, "max_response_time": 2703, "method": "", "min_response_time": 274, "name": "Total",
			"num_failures": 0, "num_none_requests": 0, "num_requests": 2, "start_time": 1723188340172, "total_content_length": 1208,
			"total_response_time": 2977,
		},
		"errors": map[string]interface{}{},
	}
	for k, v := range data {
		d[k], _ = json.Marshal(v)
	}
	runner.handleStat(d)

	if runner.stats.transactionPassed != 10 || runner.stats.transactionFailed != 2 {
		t.Error("runner stats transactions not as expected")
	}
	if runner.stats.total.StartTime != 1723188340172 {
		t.Error("runner.stats.total not as expected")
	}

}
