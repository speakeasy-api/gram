package toolconfig

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
)

// SecuritySchemeKind categorizes what kind of credential a security scheme needs.
type SecuritySchemeKind int

const (
	SecuritySchemeAPIKey SecuritySchemeKind = iota
	SecuritySchemeHTTPBearer
	SecuritySchemeHTTPBasic
	SecuritySchemeOAuth2AuthCode
	SecuritySchemeOAuth2ClientCredentials
	SecuritySchemeOpenIDConnect
)

// SecurityRequirement describes one security scheme that can satisfy a tool's auth.
type SecurityRequirement struct {
	Key          string
	Kind         SecuritySchemeKind
	EnvVariables []string
}

// DescribeToolSecurity converts a toolset's SecurityVariables into SecurityRequirements,
// classifying each by kind. This works for any tool type since SecurityVariables are
// aggregated at the toolset level from all tool sources (HTTP, function, external MCP).
func DescribeToolSecurity(secVars []*types.SecurityVariable) []SecurityRequirement {
	if len(secVars) == 0 {
		return nil
	}

	schemes := make([]SecurityRequirement, 0, len(secVars))
	for _, sv := range secVars {
		kind := classifySecurityVariable(sv)
		schemes = append(schemes, SecurityRequirement{
			Key:          sv.ID,
			Kind:         kind,
			EnvVariables: sv.EnvVariables,
		})
	}

	return schemes
}

func classifySecurityVariable(sv *types.SecurityVariable) SecuritySchemeKind {
	secType := ""
	if sv.Type != nil {
		secType = *sv.Type
	}

	switch {
	case secType == "apiKey":
		return SecuritySchemeAPIKey
	case secType == "http" && sv.Scheme == "bearer":
		return SecuritySchemeHTTPBearer
	case secType == "http" && sv.Scheme == "basic":
		return SecuritySchemeHTTPBasic
	case secType == "oauth2" && slices.Contains(sv.OauthTypes, "client_credentials"):
		return SecuritySchemeOAuth2ClientCredentials
	case secType == "oauth2":
		return SecuritySchemeOAuth2AuthCode
	case secType == "openIdConnect":
		return SecuritySchemeOpenIDConnect
	default:
		return SecuritySchemeAPIKey
	}
}

// SchemeSatisfied returns true if the given merged env has all required env vars
// populated for this scheme.
func SchemeSatisfied(scheme SecurityRequirement, mergedEnv *CaseInsensitiveEnv, oauthToken string) bool {
	switch scheme.Kind {
	case SecuritySchemeAPIKey, SecuritySchemeHTTPBearer:
		if len(scheme.EnvVariables) == 0 {
			return false
		}
		return mergedEnv.Get(scheme.EnvVariables[0]) != ""

	case SecuritySchemeHTTPBasic:
		if len(scheme.EnvVariables) < 2 {
			return false
		}
		return mergedEnv.Get(scheme.EnvVariables[0]) != "" && mergedEnv.Get(scheme.EnvVariables[1]) != ""

	case SecuritySchemeOAuth2AuthCode, SecuritySchemeOpenIDConnect:
		// For external MCP tools, the oauth token is provided directly (no env var)
		if len(scheme.EnvVariables) == 0 {
			return oauthToken != ""
		}
		for _, envVar := range scheme.EnvVariables {
			if strings.HasSuffix(strings.ToUpper(envVar), "ACCESS_TOKEN") && mergedEnv.Get(envVar) != "" {
				return true
			}
		}
		// Fall back to checking the direct oauth token
		return oauthToken != ""

	case SecuritySchemeOAuth2ClientCredentials:
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

// AnySchemeSatisfied returns true if at least one of the given schemes is fully satisfied.
// Returns true if schemes is empty (the tool has no auth requirements).
func AnySchemeSatisfied(schemes []SecurityRequirement, mergedEnv *CaseInsensitiveEnv, oauthToken string) bool {
	if len(schemes) == 0 {
		return true
	}

	for _, scheme := range schemes {
		if SchemeSatisfied(scheme, mergedEnv, oauthToken) {
			return true
		}
	}

	return false
}
