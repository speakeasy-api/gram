package mcpconnectionattempts

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// LabelOther stands in for any provider, app or client that is not on a fixed list.
const LabelOther = "other"

// knownProviderHosts are issuer hosts one vendor serves to every customer, so none can
// name a customer. A host with a tenant part, such as an Okta or Salesforce subdomain,
// must never be added: long-term rows would hold it for 13 months.
var knownProviderHosts = map[string]struct{}{
	"accounts.google.com":       {},
	"login.microsoftonline.com": {},
	"github.com":                {},
	"slack.com":                 {},
	"linear.app":                {},
	"mcp.linear.app":            {},
	"mcp.notion.com":            {},
	"api.notion.com":            {},
	"auth.atlassian.com":        {},
	"mcp.atlassian.com":         {},
	"mcp.stripe.com":            {},
	"connect.stripe.com":        {},
	"mcp.sentry.dev":            {},
	"sentry.io":                 {},
	"mcp.asana.com":             {},
	"app.asana.com":             {},
	"mcp.hubspot.com":           {},
	"app.hubspot.com":           {},
	"mcp.intercom.com":          {},
	"mcp.vercel.com":            {},
	"vercel.com":                {},
	"mcp.cloudflare.com":        {},
	"dash.cloudflare.com":       {},
	"mcp.figma.com":             {},
	"www.figma.com":             {},
	"mcp.box.com":               {},
	"account.box.com":           {},
	"mcp.zapier.com":            {},
	"mcp.supabase.com":          {},
	"api.supabase.com":          {},
	"mcp.monday.com":            {},
	"auth.monday.com":           {},
	"mcp.canva.com":             {},
	"mcp.paypal.com":            {},
	"mcp.squareup.com":          {},
	"app.getgram.ai":            {},
	"id.speakeasy.com":          {},
	"ai.speakeasy.com":          {},
}

// IsAppKind reports whether an app_key names a kind of app rather than an app.
func IsAppKind(appKey string) bool {
	return slices.Contains(appKinds, App{appKey})
}

// catalogAppHosts are the hosts of every client metadata document in the client catalog,
// disabled ones included, since a disabled entry still names a vendor.
var catalogAppHosts = buildCatalogAppHosts()

func buildCatalogAppHosts() map[string]struct{} {
	hosts := map[string]struct{}{}
	for _, preset := range admission.Catalog() {
		if host := NormalizeProviderHost(preset.URL); host != "" {
			hosts[host] = struct{}{}
		}
	}
	return hosts
}

// ProviderLabel keeps a provider host only when it is a known vendor's shared host.
// Empty stays empty: the attempt reached no provider.
func ProviderLabel(host string) string {
	if host == "" {
		return ""
	}
	if _, ok := knownProviderHosts[host]; ok {
		return host
	}
	return LabelOther
}

// AppLabel keeps an app_key only when it is a catalog app's host or a kind of app.
func AppLabel(appKey string) string {
	if appKey == "" {
		return ""
	}
	if _, ok := catalogAppHosts[appKey]; ok || IsAppKind(appKey) {
		return appKey
	}
	return LabelOther
}

// ClientFamilyLabel keeps a client family only when the writer can produce it.
// Empty stays empty: no client made the request.
func ClientFamilyLabel(family string) string {
	if family == "" || family == FamilyOther {
		return family
	}
	for _, candidate := range clientFamilies {
		if candidate.family == family {
			return family
		}
	}
	return LabelOther
}

// StepLabel keeps a step only when it is a known Step.
func StepLabel(step string) string {
	if slices.Contains(allSteps, Step{step}) {
		return step
	}
	return LabelOther
}

// OutcomeLabel keeps an outcome only when it is a known Outcome.
func OutcomeLabel(outcome string) string {
	if slices.Contains(allOutcomes, Outcome(outcome)) {
		return outcome
	}
	return LabelOther
}

// ReasonLabel keeps a reason only when it is a catalog code. Empty stays empty: nothing went wrong.
func ReasonLabel(reason string) string {
	if reason == "" {
		return ""
	}
	if _, ok := MeaningOf(reason); ok {
		return reason
	}
	return LabelOther
}

// FixedByLabel keeps a fixed_by only when it is a known FixedBy.
func FixedByLabel(fix FixedBy) string {
	if slices.Contains(fixedByValues, fix) {
		return string(fix)
	}
	return LabelOther
}
