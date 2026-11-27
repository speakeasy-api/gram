package shared

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// Enums whose values the authorization registry owns. Reading them from the
// registry keeps the API from drifting when a scope, resource kind or
// selector value is added.

func ScopeEnum() { Enum(enumValues(authz.ScopeNames())...) }

func ExclusionScopeEnum() { Enum(enumValues(authz.ExclusionScopeNames())...) }

func UserVisibleScopeEnum() { Enum(enumValues(authz.UserVisibleScopeNames())...) }

func ResourceKindEnum() { Enum(enumValues(authz.ResourceKinds())...) }

// SelectorResourceKindEnum also accepts the wildcard a selector can name.
func SelectorResourceKindEnum() {
	Enum(enumValues(append(authz.ResourceKinds(), authz.WildcardResource))...)
}

func ScopeVisibilityEnum() {
	Enum(authz.ScopeVisibilityUserVisible, authz.ScopeVisibilityInternal)
}

func ScopeCategoryEnum() { Enum(enumValues(authz.ScopeCategories)...) }

func SensitivityEnum() { Enum(enumValues(authz.Sensitivities)...) }

func DispositionEnum() { Enum(enumValues(authz.Dispositions())...) }

func AuthClientTypeEnum() { Enum(enumValues(authz.AuthClientTypes())...) }

// SelectorAuthClientTypeEnum also accepts the wildcard a selector can name.
func SelectorAuthClientTypeEnum() {
	Enum(enumValues(append(authz.AuthClientTypes(), authz.WildcardResource))...)
}

// enumValues converts to plain strings: Goa refuses enum values of a named
// string type.
func enumValues[T ~string](vals []T) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = string(v)
	}
	return out
}
