package authzcases

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// DispositionPath is where the disposition cases are written, relative to the
// server directory.
const DispositionPath = "../client/dashboard/src/pages/access/disposition-cases.gen.json"

// toolHints are a tool's annotation hints as the dashboard receives them. A
// hint the tool does not declare is left out.
type toolHints struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

// dispositionCase is one set of hints and the disposition the server gives a
// tool that carries them.
type dispositionCase struct {
	Hints       toolHints `json:"hints"`
	Disposition string    `json:"disposition"`
}

// DispositionFile renders the server's disposition for every combination of
// hints, each one absent, false or true, so the dashboard's classifier can be
// checked against it.
//
// The nested loops are bounded and cannot cause performance trouble: four
// hints with three values each is always 3^4 = 81 cases.
func DispositionFile() ([]byte, error) {
	values := []*bool{nil, new(false), new(true)}
	var b bytes.Buffer
	b.WriteString("[\n")
	first := true
	for _, readOnly := range values {
		for _, destructive := range values {
			for _, idempotent := range values {
				for _, openWorld := range values {
					disposition, _ := authz.DispositionFromHints(readOnly, destructive, idempotent, openWorld)
					c := dispositionCase{
						Hints:       toolHints{ReadOnlyHint: readOnly, DestructiveHint: destructive, IdempotentHint: idempotent, OpenWorldHint: openWorld},
						Disposition: disposition,
					}
					line, err := json.Marshal(c)
					if err != nil {
						return nil, fmt.Errorf("encode disposition case: %w", err)
					}
					if !first {
						b.WriteString(",\n")
					}
					first = false
					b.Write(line)
				}
			}
		}
	}
	b.WriteString("\n]\n")
	return b.Bytes(), nil
}
