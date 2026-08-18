package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
)

const (
	MetaGramKind     = "gram.ai/kind"
	MetaGramMimeType = "getgram.ai/mime-type"

	// metaKeyServerInfo is the _meta key reserved by MCP 2026-07-28 for the
	// server to identify itself on every result under a stateless protocol.
	metaKeyServerInfo = "io.modelcontextprotocol/serverInfo"

	// resultTypeComplete is the resultType MCP 2026-07-28 requires on every
	// ordinary result. Gram implements no multi round-trip requests, so it is
	// the only value Gram produces.
	resultTypeComplete = "complete"
)

// Server identities injected into every result's _meta and answered from the
// two initialize handlers. serverInfo is display/debug only per the MCP spec,
// so the static version carries no compatibility meaning; keeping it constant
// also keeps the initialize response and per-result _meta in agreement.
var (
	serverInfoHostedToolset   = serverInfo{Name: "Gram", Version: "0.0.0"}
	serverInfoPlatformToolset = serverInfo{Name: "Gram Platform Toolset", Version: "0.0.0"}
)

var (
	errInvalidJSONRPCVersion = errors.New("invalid json-rpc version")
)

type resultEnvelope[T any] struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      mcpjsonrpc.ID `json:"id"`
	Result  T             `json:"result"`
}

type result[T any] struct {
	ID     mcpjsonrpc.ID `json:"id"`
	Result T             `json:"result"`

	// serverIdentity is injected into the result's _meta as the server's
	// identity. A zero value falls back to the hosted-toolset identity at
	// marshal time.
	serverIdentity serverInfo
}

func (m result[T]) MarshalJSON() ([]byte, error) {
	resultBytes, err := json.Marshal(m.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	identity := m.serverIdentity
	if identity.Name == "" {
		identity = serverInfoHostedToolset
	}

	spliced, err := spliceResultProtocolFields(resultBytes, identity)
	if err != nil {
		return nil, fmt.Errorf("splice result protocol fields: %w", err)
	}

	bs, err := json.Marshal(resultEnvelope[json.RawMessage]{
		JSONRPC: "2.0",
		ID:      m.ID,
		Result:  spliced,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal result envelope: %w", err)
	}

	return bs, nil
}

// spliceResultProtocolFields injects the fields MCP 2026-07-28 defines on
// every result: a top-level resultType and the server identity under _meta.
// Every earlier revision permits extra result fields, so both are emitted
// unconditionally. Both are fill-if-missing so a result relayed from an
// upstream MCP server keeps whatever the upstream already supplied.
//
// A result that is not a JSON object — possible only when an MCP-passthrough
// tool returns spec-violating output — is returned unchanged: the malformed
// shape stays the upstream's problem rather than becoming a Gram
// serialization failure. On the spliced path values are preserved
// semantically rather than byte-for-byte: top-level (and _meta) keys are
// re-marshaled in sorted order, and insignificant whitespace and HTML
// escaping inside values may change, exactly as the pre-splice envelope
// already did to passthrough bodies.
func spliceResultProtocolFields(resultBytes []byte, identity serverInfo) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(resultBytes, &fields); err != nil || fields == nil {
		return resultBytes, nil
	}

	if _, ok := fields["resultType"]; !ok {
		fields["resultType"] = json.RawMessage(strconv.Quote(resultTypeComplete))
	}

	meta := map[string]json.RawMessage{}
	injectMeta := true
	if rawMeta, ok := fields["_meta"]; ok {
		// A _meta holding anything other than an object or null is left
		// untouched; resultType is still injected. A null _meta is
		// equivalent to an absent one, so it is replaced the same way.
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			injectMeta = false
		} else if meta == nil {
			meta = map[string]json.RawMessage{}
		}
	}
	if injectMeta {
		if _, ok := meta[metaKeyServerInfo]; !ok {
			identityBytes, err := json.Marshal(identity)
			if err != nil {
				return nil, fmt.Errorf("marshal server info: %w", err)
			}
			meta[metaKeyServerInfo] = identityBytes
		}
		metaBytes, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("marshal result _meta: %w", err)
		}
		fields["_meta"] = metaBytes
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal result fields: %w", err)
	}

	return out, nil
}

func (m *result[T]) UnmarshalJSON(data []byte) error {
	var envelope resultEnvelope[T]
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("unmarshal result envelope: %w", err)
	}

	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("%w: %s", errInvalidJSONRPCVersion, envelope.JSONRPC)
	}

	m.ID = envelope.ID
	m.Result = envelope.Result
	m.serverIdentity = serverInfo{Name: "", Version: ""}

	return nil
}

type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      mcpjsonrpc.ID   `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func respondWithNoContent(ack bool, w http.ResponseWriter) error {
	acks := strconv.FormatBool(ack)
	w.Header().Set("Noop", acks)
	w.WriteHeader(http.StatusAccepted)
	return nil
}
