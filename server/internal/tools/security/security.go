package security

import (
	"encoding/json"
	"fmt"
)

type SecurityData []map[string][]string

func ParseHTTPToolSecurityKeys(securityPayload []byte) ([]string, error) {
	if len(securityPayload) == 0 {
		return []string{}, nil
	}

	var securityData SecurityData
	if err := json.Unmarshal(securityPayload, &securityData); err != nil {
		return nil, fmt.Errorf("parse security data: %w", err)
	}

	keys := make([]string, 0)
	for _, securityMap := range securityData {
		for key := range securityMap {
			keys = append(keys, key)
		}
	}

	return keys, nil
}
