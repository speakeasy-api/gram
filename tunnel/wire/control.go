package wire

// ControlPathPrefix reserves HTTP paths carried over yamux, not a side channel.
const ControlPathPrefix = "/_tunnel/"

// ControlHelloPath is sent by the gateway after registering a session.
// A graceful-drain control frame was sketched here previously but never had a
// gateway-side sender; reintroduce it alongside the sender when pod drain is
// actually implemented.
const ControlHelloPath = ControlPathPrefix + "hello"

type HelloFrame struct {
	Type      string `json:"type"`
	TunnelID  string `json:"tunnel_id"`
	SessionID string `json:"session_id"`
}
