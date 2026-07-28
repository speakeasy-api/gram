package chat

import "github.com/google/uuid"

// agentSessionNamespace is the UUIDv5 namespace agent session ids are hashed
// under when they are not themselves UUIDs. It is fixed forever: changing it
// would re-key every chat already persisted under it.
var agentSessionNamespace = uuid.NameSpaceDNS

// SessionIDToChatID maps an agent session id onto the chat id its transcript is
// stored under. A session id that is already a UUID is the chat id; anything
// else becomes a deterministic UUIDv5 of the raw string.
//
// This is the only mapping the capture paths use, so any consumer that starts
// from a raw session id — telemetry, efficacy scoring — must resolve the chat
// through here rather than matching the raw string against a chat column.
func SessionIDToChatID(sessionID string) uuid.UUID {
	if parsed, err := uuid.Parse(sessionID); err == nil {
		return parsed
	}

	return uuid.NewSHA1(agentSessionNamespace, []byte(sessionID))
}
