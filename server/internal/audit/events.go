package audit

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type subjectType string

const (
	subjectTypeAPIKey                   subjectType = "api_key"
	subjectTypeAccessMember             subjectType = "access_member"
	subjectTypeAccessRole               subjectType = "access_role"
	subjectTypeAssistantMemory          subjectType = "assistant_memory"
	subjectTypeAsset                    subjectType = "asset"
	subjectTypeCustomDomain             subjectType = "custom_domain"
	subjectTypeDeployment               subjectType = "deployment"
	subjectTypeEnvironment              subjectType = "environment"
	subjectTypeMcpCollection            subjectType = "mcp_collection"
	subjectTypeMcpEndpoint              subjectType = "mcp_endpoint"
	subjectTypeMcpServer                subjectType = "mcp_server"
	subjectTypeOtelForwarding           subjectType = "otel_forwarding_config"
	subjectTypeOrganizationInvite       subjectType = "organization_invitation"
	subjectTypePlugin                   subjectType = "plugin"
	subjectTypeProject                  subjectType = "project"
	subjectTypeTemplate                 subjectType = "template"
	subjectTypeRemoteMcpServer          subjectType = "remote_mcp_server"
	subjectTypeToolset                  subjectType = "toolset"
	subjectTypeTriggerInstance          subjectType = "trigger_instance"
	subjectTypeVariation                subjectType = "variation"
	subjectTypeRiskPolicy               subjectType = "risk_policy"
	subjectTypeShadowMCPAccessRule      subjectType = "shadow_mcp_access_rule"
	subjectTypeShadowMCPApprovalRequest subjectType = "shadow_mcp_approval_request"
	subjectTypeAccessChallenge          subjectType = "access_challenge"
	subjectTypeUserSession              subjectType = "user_session"
	subjectTypeUserSessionClient        subjectType = "user_session_client"
	subjectTypeUserSessionConsent       subjectType = "user_session_consent"
	subjectTypeUserSessionIssuer        subjectType = "user_session_issuer"
	subjectTypeRemoteSession            subjectType = "remote_session"
	subjectTypeRemoteSessionClient      subjectType = "remote_session_client"
	subjectTypeRemoteSessionIssuer      subjectType = "remote_session_issuer"
)

type Action string

func marshalAuditPayload(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if rv.IsNil() {
			return nil, nil
		}
	default:
	}

	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal audit payload: %w", err)
	}

	return b, nil
}
