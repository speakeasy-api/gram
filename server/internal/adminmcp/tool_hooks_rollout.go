//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errHooksRolloutUnavailable = errors.New("hooks rollout state is unavailable")

// maxHooksRolloutOverrides bounds the overrides one call returns. Overrides
// are exceptions to the default pin, so a handful is normal and a hundred
// already means the default pin is not doing its job.
const maxHooksRolloutOverrides = 100

// HooksRolloutReader exposes the dashboard's hooks version rollout reads.
type HooksRolloutReader interface {
	GetHooksRollout(context.Context, *gen.GetHooksRolloutPayload) (*gen.AdminHooksRollout, error)
	GetOrganizationHooksRollout(context.Context, *gen.GetOrganizationHooksRolloutPayload) (*gen.AdminOrganizationHooksRollout, error)
}

// HooksRolloutPin is a hooks rollout pin.
type HooksRolloutPin struct {
	Version int    `json:"version"`
	SetBy   string `json:"set_by"`
	SetAt   string `json:"set_at"`
}

// HooksRolloutOverride is an organization pinned apart from the default pin.
type HooksRolloutOverride struct {
	OrganizationID   string          `json:"organization_id"`
	OrganizationSlug string          `json:"organization_slug"`
	Pin              HooksRolloutPin `json:"pin"`
}

// HooksRolloutChange is one change to a pin. OrganizationID is empty for a
// change to the default pin, and Version is nil when an override was cleared.
type HooksRolloutChange struct {
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationSlug string `json:"organization_slug,omitempty"`
	Version          *int   `json:"version,omitempty"`
	SetBy            string `json:"set_by"`
	SetAt            string `json:"set_at"`
}

// HooksRollout is the platform-wide hooks version rollout state.
type HooksRollout struct {
	CurrentVersion          int                    `json:"current_version"`
	DefaultPin              *HooksRolloutPin       `json:"default_pin,omitempty"`
	CanaryOrganizationSlugs []string               `json:"canary_organization_slugs"`
	Overrides               []HooksRolloutOverride `json:"overrides"`
	OverridesTruncated      bool                   `json:"overrides_truncated"`
	RecentChanges           []HooksRolloutChange   `json:"recent_changes"`
}

// OrganizationHooksRollout is one organization's hooks version rollout state.
type OrganizationHooksRollout struct {
	OrganizationID   string           `json:"organization_id"`
	CurrentVersion   int              `json:"current_version"`
	Source           string           `json:"source"`
	EffectiveVersion *int             `json:"effective_version,omitempty"`
	Eligible         *bool            `json:"eligible,omitempty"`
	Override         *HooksRolloutPin `json:"override,omitempty"`
	DefaultPin       *HooksRolloutPin `json:"default_pin,omitempty"`
}

func hooksRolloutPin(pin *gen.AdminHooksRolloutPin) *HooksRolloutPin {
	if pin == nil {
		return nil
	}
	return &HooksRolloutPin{Version: pin.Version, SetBy: pin.SetBy, SetAt: pin.SetAt}
}

func registerHooksRolloutTools(server *mcp.Server, organizations OrganizationReader, rollout HooksRolloutReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_hooks_rollout",
		Title:       "Get Hooks Version Rollout",
		Description: "Read the platform-wide hooks version rollout: the hooks generator version this server build publishes, the default pin every organization without an override follows, canary organizations, organization overrides and recent pin changes. An organization's hooks plugin moves to the current version on the next hourly rollout sweep once its pin reaches it. Does not change pins.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, HooksRollout, error) {
		if rollout == nil {
			return nil, HooksRollout{}, errHooksRolloutUnavailable
		}
		result, err := rollout.GetHooksRollout(ctx, &gen.GetHooksRolloutPayload{AdminSessionToken: nil})
		if err != nil || result == nil {
			return nil, HooksRollout{}, errHooksRolloutUnavailable
		}

		overrides := make([]HooksRolloutOverride, 0, min(len(result.Overrides), maxHooksRolloutOverrides))
		for _, override := range result.Overrides[:min(len(result.Overrides), maxHooksRolloutOverrides)] {
			if override == nil || override.Pin == nil {
				continue
			}
			overrides = append(overrides, HooksRolloutOverride{
				OrganizationID:   override.OrganizationID,
				OrganizationSlug: override.OrganizationSlug,
				Pin:              *hooksRolloutPin(override.Pin),
			})
		}
		changes := make([]HooksRolloutChange, 0, len(result.RecentChanges))
		for _, change := range result.RecentChanges {
			if change == nil {
				continue
			}
			item := HooksRolloutChange{Version: change.Version, SetBy: change.SetBy, SetAt: change.SetAt}
			if change.OrganizationID != nil {
				item.OrganizationID = *change.OrganizationID
			}
			if change.OrganizationSlug != nil {
				item.OrganizationSlug = *change.OrganizationSlug
			}
			changes = append(changes, item)
		}

		return nil, HooksRollout{
			CurrentVersion:          result.CurrentVersion,
			DefaultPin:              hooksRolloutPin(result.DefaultPin),
			CanaryOrganizationSlugs: append([]string{}, result.CanaryOrganizationSlugs...),
			Overrides:               overrides,
			OverridesTruncated:      len(result.Overrides) > maxHooksRolloutOverrides,
			RecentChanges:           changes,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_hooks_rollout",
		Title:       "Get Organization Hooks Version Rollout",
		Description: "Read which hooks version an exact organization ID is cleared for and why: canary, its own override, the default pin, or legacy_flag when no pin applies yet and the legacy PostHog flag decides (eligibility is then unknown here). eligible reports whether its next publish moves its hooks plugin to current_version. Does not change pins.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, OrganizationHooksRollout, error) {
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationHooksRollout{}, err
		}
		if rollout == nil {
			return nil, OrganizationHooksRollout{}, errHooksRolloutUnavailable
		}
		result, err := rollout.GetOrganizationHooksRollout(ctx, &gen.GetOrganizationHooksRolloutPayload{AdminSessionToken: nil, OrganizationID: org.ID})
		if err != nil || result == nil || result.OrganizationID != org.ID {
			return nil, OrganizationHooksRollout{}, errHooksRolloutUnavailable
		}
		return nil, OrganizationHooksRollout{
			OrganizationID:   org.ID,
			CurrentVersion:   result.CurrentVersion,
			Source:           result.Source,
			EffectiveVersion: result.EffectiveVersion,
			Eligible:         result.Eligible,
			Override:         hooksRolloutPin(result.Override),
			DefaultPin:       hooksRolloutPin(result.DefaultPin),
		}, nil
	})
}
