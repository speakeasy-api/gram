//nolint:exhaustruct_v5 // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

const functionTimeoutSideEffects = "Changes how long each Speakeasy Function tool call and resource read may run for every project in the organization, from the next call; calls already running keep their limit. A call that runs out of time fails with a timeout error. A limit above 5 minutes applies to a project only after it is redeployed, because earlier function runners stop calls at 5 minutes; this operation does not redeploy. Does not change any other organization setting. Recorded in the staff write log, not the organization's audit log."

var (
	errFunctionTimeoutNoChange = errors.New("organization already has the requested function timeout; nothing would change")
	errFunctionTimeoutInput    = fmt.Errorf("set function_timeout_seconds to a whole number from %d to %d, or set use_default to true and omit function_timeout_seconds", constants.MinFunctionTimeoutSeconds, constants.MaxFunctionTimeoutSeconds)
)

// FunctionTimeoutInput is one exact function timeout change.
type FunctionTimeoutInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`

	// FunctionTimeoutSeconds is the new limit. It must be zero when
	// UseDefault is set.
	FunctionTimeoutSeconds int `json:"function_timeout_seconds,omitempty" jsonschema:"Limit in seconds on one function invocation, a whole number from 30 to 600. Omit when use_default is true"`

	// UseDefault clears a staff-set limit so the platform default applies.
	UseDefault bool `json:"use_default,omitempty" jsonschema:"Set to true, and omit function_timeout_seconds, to clear the staff-set limit and restore the 60-second platform default"`
}

type functionTimeoutReceipt struct {
	Before      admin.OrganizationFunctionTimeoutState `json:"before"`
	After       admin.OrganizationFunctionTimeoutState `json:"after"`
	SideEffects string                                 `json:"side_effects"`
}

type functionTimeoutWriter struct {
	runner *writeRunner
	writes WriteConfig
}

// requestedSeconds validates the input and returns the value to store: nil
// restores the default.
func (in FunctionTimeoutInput) requestedSeconds() (*int, error) {
	if in.UseDefault {
		if in.FunctionTimeoutSeconds != 0 {
			return nil, errFunctionTimeoutInput
		}
		return nil, nil
	}
	if !constants.IsFunctionTimeoutSeconds(in.FunctionTimeoutSeconds) {
		return nil, errFunctionTimeoutInput
	}
	return &in.FunctionTimeoutSeconds, nil
}

func (w *functionTimeoutWriter) apply(ctx context.Context, input FunctionTimeoutInput) (functionTimeoutReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationSetOrganizationFunctionTimeout)
	if err != nil {
		return functionTimeoutReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return functionTimeoutReceipt{}, err
	}
	seconds, err := input.requestedSeconds()
	if err != nil {
		return functionTimeoutReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (functionTimeoutReceipt, error) {
		before, err := admin.LockOrganizationFunctionTimeoutTx(ctx, tx, input.OrganizationID)
		if errors.Is(err, admin.ErrOrganizationFunctionTimeoutNotFound) {
			return functionTimeoutReceipt{}, ErrStaleState
		}
		if err != nil {
			return functionTimeoutReceipt{}, fmt.Errorf("read organization function timeout: %w", err)
		}
		if (seconds == nil && before.IsDefault) || (seconds != nil && !before.IsDefault && before.Seconds == *seconds) {
			return functionTimeoutReceipt{}, errFunctionTimeoutNoChange
		}
		after, err := admin.SetOrganizationFunctionTimeoutTx(ctx, tx, before, seconds)
		if err != nil {
			return functionTimeoutReceipt{}, fmt.Errorf("set organization function timeout: %w", err)
		}
		return functionTimeoutReceipt{Before: before, After: after, SideEffects: functionTimeoutSideEffects}, nil
	})
}

func (w *functionTimeoutWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationSetOrganizationFunctionTimeout), Title: "Set Organization Function Timeout", Description: "Set how long one Speakeasy Function tool call or resource read may run for one exact canonical organization ID, from 30 to 600 seconds, or restore the 60-second platform default with use_default. " + functionTimeoutSideEffects + " Read get_organization_summary first and show the user the current and requested limits."}, w.apply)
}
