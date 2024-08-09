package boomer

const (
	typeClientReady      = "client_ready"
	typeClientStopped    = "client_stopped"
	typeHeartbeat        = "heartbeat"
	typeSpawning         = "spawning"
	typeSpawningComplete = "spawning_complete"
	typeQuit             = "quit"
	typeException        = "exception"
	typeStats            = "stats"
)

type genericMessage struct {
	Type    string            `json:"type,omitempty"`
	Profile []byte            `json:"profile,omitempty"`
	Data    map[string][]byte `json:"data,omitempty"`
	NodeID  string            `json:"node_id,omitempty"`
	Tasks   []byte            `json:"tasks,omitempty"`
}

type task struct {
	Profile        *Profile `json:"profile,omitempty"`
	TestCasesBytes []byte   `json:"testcases,omitempty"`
}

func newGenericMessage(t string, data map[string][]byte, nodeID string) (msg *genericMessage) {
	return &genericMessage{
		Type:   t,
		Data:   data,
		NodeID: nodeID,
	}
}

func newQuitMessage(nodeID string) (msg *genericMessage) {
	return &genericMessage{
		Type:   "quit",
		NodeID: nodeID,
	}
}

func newMessageToWorker(t string, profile []byte, data map[string][]byte, tasks []byte) (msg *genericMessage) {
	return &genericMessage{
		Type:    t,
		Profile: profile,
		Data:    data,
		Tasks:   tasks,
	}
}

func newClientReadyMessageToMaster(nodeID string) (msg *genericMessage) {
	return &genericMessage{
		Type:   "client_ready",
		NodeID: nodeID,
	}
}
