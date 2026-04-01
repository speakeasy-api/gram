package mcp

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// securitySchemeKind categorizes what kind of credential a security scheme needs.
type securitySchemeKind int

const (
	securitySchemeAPIKey securitySchemeKind = iota
	securitySchemeHTTPBearer
	securitySchemeHTTPBasic
	securitySchemeOAuth2AuthCode
	securitySchemeOAuth2ClientCredentials
	securitySchemeOpenIDConnect
)

// securityRequirement describes one security scheme that can satisfy a tool's auth.
type securityRequirement struct {
	Key          string
	Kind         securitySchemeKind
	EnvVariables []string
}

// describeToolSecurity converts a toolset's SecurityVariables into securityRequirements,
// classifying each by kind.
func describeToolSecurity(secVars []*types.SecurityVariable) []securityRequirement {
	if len(secVars) == 0 {
		return nil
	}

	schemes := make([]securityRequirement, 0, len(secVars))
	for _, sv := range secVars {
		kind := classifySecurityVariable(sv)
		schemes = append(schemes, securityRequirement{
			Key:          sv.ID,
			Kind:         kind,
			EnvVariables: sv.EnvVariables,
		})
	}

	return schemes
}

func classifySecurityVariable(sv *types.SecurityVariable) securitySchemeKind {
	secType := ""
	if sv.Type != nil {
		secType = *sv.Type
	}

	switch {
	case secType == "apiKey":
		return securitySchemeAPIKey
	case secType == "http" && sv.Scheme == "bearer":
		return securitySchemeHTTPBearer
	case secType == "http" && sv.Scheme == "basic":
		return securitySchemeHTTPBasic
	case secType == "oauth2" && slices.Contains(sv.OauthTypes, "client_credentials"):
		return securitySchemeOAuth2ClientCredentials
	case secType == "oauth2":
		return securitySchemeOAuth2AuthCode
	case secType == "openIdConnect":
		return securitySchemeOpenIDConnect
	default:
		return securitySchemeAPIKey
	}
}

// schemeSatisfied returns true if the given merged env has all required env vars
// populated for this scheme.
func schemeSatisfied(scheme securityRequirement, mergedEnv *toolconfig.CaseInsensitiveEnv, oauthToken string) bool {
	switch scheme.Kind {
	case securitySchemeAPIKey, securitySchemeHTTPBearer:
		if len(scheme.EnvVariables) == 0 {
			return false
		}
		return mergedEnv.Get(scheme.EnvVariables[0]) != ""

	case securitySchemeHTTPBasic:
		if len(scheme.EnvVariables) < 2 {
			return false
		}
		return mergedEnv.Get(scheme.EnvVariables[0]) != "" && mergedEnv.Get(scheme.EnvVariables[1]) != ""

	case securitySchemeOAuth2AuthCode, securitySchemeOpenIDConnect:
		if len(scheme.EnvVariables) == 0 {
			return oauthToken != ""
		}
		for _, envVar := range scheme.EnvVariables {
			if strings.HasSuffix(strings.ToUpper(envVar), "ACCESS_TOKEN") && mergedEnv.Get(envVar) != "" {
				return true
			}
		}
		return oauthToken != ""

	case securitySchemeOAuth2ClientCredentials:
		hasClientID := false
		hasClientSecret := false
		for _, envVar := range scheme.EnvVariables {
			upper := strings.ToUpper(envVar)
			if strings.HasSuffix(upper, "CLIENT_ID") && mergedEnv.Get(envVar) != "" {
				hasClientID = true
			}
			if strings.HasSuffix(upper, "CLIENT_SECRET") && mergedEnv.Get(envVar) != "" {
				hasClientSecret = true
			}
		}
		return hasClientID && hasClientSecret

	default:
		return false
	}
}

// anySchemeSatisfied returns true if at least one of the given schemes is fully satisfied.
// Returns true if schemes is empty (the tool has no auth requirements).
func anySchemeSatisfied(schemes []securityRequirement, mergedEnv *toolconfig.CaseInsensitiveEnv, oauthToken string) bool {
	if len(schemes) == 0 {
		return true
	}

	for _, scheme := range schemes {
		if schemeSatisfied(scheme, mergedEnv, oauthToken) {
			return true
		}
	}

	return false
}
