package wire

// Control frames are exchanged as ordinary HTTP requests over the multiplexed
// session (path prefix ControlPathPrefix), not a bespoke channel. This keeps
// the agent a single `http.Serve(session, handler)` — every substream is HTTP,
// control included — and lets yamux keepalive handle liveness.
const ControlPathPrefix = "/_tunnel/"

const (
	// ControlHelloPath: gateway -> agent right after connect, carries HelloFrame.
	ControlHelloPath = ControlPathPrefix + "hello"
	// ControlDrainPath: gateway -> agent when the pod is terminating; the agent
	// should open a fresh connection (lands on a surviving pod) and let in-flight
	// work finish on the old session.
	ControlDrainPath = ControlPathPrefix + "drain"
)

// HelloFrame is sent by the gateway to the agent immediately after a session is
// registered.
type HelloFrame struct {
	Type       string `json:"type"`
	TunnelID   string `json:"tunnel_id"`
	SessionID  string `json:"session_id"`
	MinVersion string `json:"min_version"`
	DrainGrace string `json:"drain_grace,omitempty"`
}

// DrainFrame is sent by the gateway to the agent on graceful shutdown.
type DrainFrame struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}
