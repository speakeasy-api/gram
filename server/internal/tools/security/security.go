package security

import (
	"encoding/json"
	"fmt"
)

type SecurityData []map[string][]string

func ParseHTTPToolSecurityKeys(securityPayload []byte) ([]string, map[string][]string, error) {
	securityScopes := make(map[string][]string)
	if len(securityPayload) == 0 {
		return []string{}, securityScopes, nil
	}

	var securityData SecurityData
	if err := json.Unmarshal(securityPayload, &securityData); err != nil {
		return nil, nil, fmt.Errorf("parse security data: %w", err)
	}

	keys := make([]string, 0)
	for _, securityMap := range securityData {
		for key := range securityMap {
			keys = append(keys, key)
			securityScopes[key] = securityMap[key]
		}
	}

	return keys, securityScopes, nil
}
