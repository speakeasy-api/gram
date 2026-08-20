package audit

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type subjectType string

const (
	subjectTypeAPIKey                      subjectType = "api_key"
	subjectTypeAccessChallenge             subjectType = "access_challenge"
	subjectTypeAccessMember                subjectType = "access_member"
	subjectTypeAccessRole                  subjectType = "access_role"
	subjectTypeAIIntegration               subjectType = "ai_integration_config"
	subjectTypeAssistant                   subjectType = "assistant"
	subjectTypeAssistantMemory             subjectType = "assistant_memory"
	subjectTypeAsset                       subjectType = "asset"
	subjectTypeAwsIamCredential            subjectType = "aws_iam_credential"
	subjectTypeAwsKmsKey                   subjectType = "aws_kms_key"
	subjectTypeBillingMetadata             subjectType = "billing_metadata"
	subjectTypeChatAnalysisSettings        subjectType = "chat_analysis_settings"
	subjectTypeChatSession                 subjectType = "chat_session"
	subjectTypeCustomDomain                subjectType = "custom_domain"
	subjectTypeDeployment                  subjectType = "deployment"
	subjectTypeDeviceIntegration           subjectType = "device_integration_config"
	subjectTypeEnvironment                 subjectType = "environment"
	subjectTypeGcpIamCredential            subjectType = "gcp_iam_credential"
	subjectTypeGcpKmsKey                   subjectType = "gcp_kms_key"
	subjectTypeLiteLLMInstance             subjectType = "litellm_instance"
	subjectTypeMcpApprovalRequest          subjectType = "mcp_approval_request"
	subjectTypeMcpCollection               subjectType = "mcp_collection"
	subjectTypeMcpEndpoint                 subjectType = "mcp_endpoint"
	subjectTypeMcpServer                   subjectType = "mcp_server"
	subjectTypeMetaMcpServer               subjectType = "meta_mcp_server"
	subjectTypeModelProviderKey            subjectType = "model_provider_key"
	subjectTypeOpenRouterAPIKey            subjectType = "openrouter_api_key"
	subjectTypeOtelForwarding              subjectType = "otel_forwarding_config"
	subjectTypeOrganizationInvite          subjectType = "organization_invitation"
	subjectTypePlatformMcpRegistration     subjectType = "platform_mcp_registration"
	subjectTypeUnproxiedMcpServer          subjectType = "unproxied_mcp_server"
	subjectTypePlugin                      subjectType = "plugin"
	subjectTypeProject                     subjectType = "project"
	subjectTypeRemoteMcpServer             subjectType = "remote_mcp_server"
	subjectTypeRemoteMcpServerHeader       subjectType = "remote_mcp_server_header"
	subjectTypeRemoteSession               subjectType = "remote_session"
	subjectTypeRemoteSessionClient         subjectType = "remote_session_client"
	subjectTypeRemoteSessionIssuer         subjectType = "remote_session_issuer"
	subjectTypeRiskExclusion               subjectType = "risk_exclusion"
	subjectTypeRiskPolicy                  subjectType = "risk_policy"
	subjectTypeRiskResult                  subjectType = "risk_result"
	subjectTypeSkill                       subjectType = "skill"
	subjectTypeSkillEfficacySettings       subjectType = "skill_efficacy_settings"
	subjectTypeSpendRule                   subjectType = "spend_rule"
	subjectTypeTemplate                    subjectType = "template"
	subjectTypeToolset                     subjectType = "toolset"
	subjectTypeTriggerInstance             subjectType = "trigger_instance"
	subjectTypeTunneledMcpServer           subjectType = "tunneled_mcp_server"
	subjectTypeUserSession                 subjectType = "user_session"
	subjectTypeUserSessionClient           subjectType = "user_session_client"
	subjectTypeUserSessionConsent          subjectType = "user_session_consent"
	subjectTypeUserSessionIssuer           subjectType = "user_session_issuer"
	subjectTypeUserSessionIssuerCimdClient subjectType = "user_session_issuer_cimd_client"
	subjectTypeVariation                   subjectType = "variation"
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
