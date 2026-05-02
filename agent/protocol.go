package main

type MessageType string

const (
	MsgStartServer     MessageType = "start_server"
	MsgStopServer      MessageType = "stop_server"
	MsgDeleteServer    MessageType = "delete_server"
	MsgRconCommand     MessageType = "rcon_command"
	MsgStreamLogsStart MessageType = "stream_logs_start"
	MsgStreamLogsStop  MessageType = "stream_logs_stop"
	MsgRegister        MessageType = "register"
	MsgHeartbeat       MessageType = "heartbeat"
	MsgServerStatus    MessageType = "server_status"
	MsgLogLine         MessageType = "log_line"
	MsgRconResponse    MessageType = "rcon_response"
	MsgApproved        MessageType = "approved"
	MsgMetrics         MessageType = "metrics"
	MsgRequestMetrics  MessageType = "request_metrics"
)

type Envelope struct {
	Type      MessageType `json:"type"`
	MessageID string      `json:"message_id,omitempty"`
	Payload   interface{} `json:"payload"`
}

type RegisterPayload struct {
	AgentName string   `json:"agent_name"`
	Token     string   `json:"token"`
	HostInfo  HostInfo `json:"host_info"`
}

type HostInfo struct {
	OS       string `json:"os"`
	CPUCores int    `json:"cpu_cores"`
	MemoryGB int    `json:"memory_gb"`
	DiskGB   int    `json:"disk_gb"`
}

type HeartbeatPayload struct {
	AgentName string `json:"agent_name"`
}

type ApprovedPayload struct {
	Token string `json:"token"`
}

type PortMapping struct {
	Label          string `json:"label"`
	ContainerPort  int    `json:"container_port"`
	Protocol       string `json:"protocol"`
	HostPortOffset int    `json:"host_port_offset"`
}

type StartServerPayload struct {
	ServerID      int               `json:"server_id"`
	ContainerName string            `json:"container_name"`
	Image         string            `json:"image"`
	Port          int               `json:"port"`
	MaxPlayers    int               `json:"max_players"`
	Env           map[string]string `json:"env"`
	PortMappings  []PortMapping     `json:"port_mappings"`
	DataPath      string            `json:"data_path"`
}

type StopServerPayload struct {
	ServerID      int    `json:"server_id"`
	ContainerName string `json:"container_name"`
}

type DeleteServerPayload struct {
	ServerID      int    `json:"server_id"`
	ContainerName string `json:"container_name"`
	VolumeName    string `json:"volume_name"`
}

type ServerStatusPayload struct {
	ServerID    int    `json:"server_id"`
	Status      string `json:"status"`
	ContainerID string `json:"container_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

type RconCommandPayload struct {
	ServerID     int    `json:"server_id"`
	MessageID    string `json:"message_id"`
	Command      string `json:"command"`
	RconHost     string `json:"rcon_host"`
	RconPort     int    `json:"rcon_port"`
	RconPassword string `json:"rcon_password"`
}

type RconResponsePayload struct {
	ServerID  int    `json:"server_id"`
	MessageID string `json:"message_id"`
	Response  string `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

type StreamLogsPayload struct {
	ServerID      int    `json:"server_id"`
	ContainerName string `json:"container_name"`
}

type LogLinePayload struct {
	ServerID int    `json:"server_id"`
	Line     string `json:"line"`
	Time     string `json:"time"`
}

type ContainerMetrics struct {
	ContainerName string  `json:"container_name"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemUsedMB     uint64  `json:"mem_used_mb"`
	MemLimitMB    uint64  `json:"mem_limit_mb"`
}

type MetricsPayload struct {
	AgentName   string             `json:"agent_name"`
	CPUPercent  float64            `json:"cpu_percent"`
	MemUsedMB   uint64             `json:"mem_used_mb"`
	MemTotalMB  uint64             `json:"mem_total_mb"`
	DiskUsedGB  uint64             `json:"disk_used_gb"`
	DiskTotalGB uint64             `json:"disk_total_gb"`
	Containers  []ContainerMetrics `json:"containers"`
	Timestamp   string             `json:"timestamp"`
}
