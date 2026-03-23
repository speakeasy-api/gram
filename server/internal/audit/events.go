package audit

import (
	"encoding/json"
	"fmt"
)

type Action string

func marshalAuditPayload(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}

	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal audit payload: %w", err)
	}

	return b, nil
}
