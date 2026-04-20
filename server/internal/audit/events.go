package audit

import (
	"encoding/json"
	"fmt"
)

type subjectType string

const (
	subjectTypeAPIKey          subjectType = "api_key"
	subjectTypeAccessMember    subjectType = "access_member"
	subjectTypeAccessRole      subjectType = "access_role"
	subjectTypeAsset           subjectType = "asset"
	subjectTypeCustomDomain    subjectType = "custom_domain"
	subjectTypeDeployment      subjectType = "deployment"
	subjectTypeEnvironment     subjectType = "environment"
	subjectTypeProject         subjectType = "project"
	subjectTypeTemplate        subjectType = "template"
	subjectTypeRemoteMcpServer subjectType = "remote_mcp_server"
	subjectTypeToolset         subjectType = "toolset"
	subjectTypeVariation       subjectType = "variation"
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
