package wire

// ControlPathPrefix reserves HTTP paths carried over yamux, not a side channel.
const ControlPathPrefix = "/_tunnel/"

const (
	// ControlHelloPath is sent by the gateway after registering a session.
	ControlHelloPath = ControlPathPrefix + "hello"
	// ControlDrainPath is sent before closing a terminating pod's session.
	ControlDrainPath = ControlPathPrefix + "drain"
)

type HelloFrame struct {
	Type       string `json:"type"`
	TunnelID   string `json:"tunnel_id"`
	SessionID  string `json:"session_id"`
	DrainGrace string `json:"drain_grace,omitempty"`
}

type DrainFrame struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}
