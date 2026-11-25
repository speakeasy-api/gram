package roledistribution_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/topics"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleRoleDistributionSetupRequestedFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("retryable database failure")
	roleURN := "role:organization:" + uuid.NewString()
	var logs bytes.Buffer
	handler := roledistribution.NewHandler(slog.New(slog.NewTextHandler(&logs, nil)), roledistribution.Processors{
		Setup: func(_ context.Context, gotRoleURN, organizationID string) (bool, error) {
			require.Equal(t, roleURN, gotRoleURN)
			require.Equal(t, "org-test", organizationID)
			return false, expected
		},
	})
	event := roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: new(roleURN), OrganizationId: new("org-test")}.Build()
	require.ErrorIs(t, handler.HandleRoleDistributionSetupRequested(t.Context(), event, gcp.MessageMetadata{ID: "message-test"}), expected)
	for _, diagnostic := range []string{expected.Error(), "message_id=message-test", "role_urn=" + roleURN, "organization_id=org-test"} {
		require.Contains(t, logs.String(), diagnostic)
	}
}

func TestHandleRoleDistributionSetupRequestedDispatch(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	for _, target := range []string{"setup", "global", "bootstrap"} {
		for _, fails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fails=%v", target, fails), func(t *testing.T) {
				t.Parallel()

				var expected error
				if fails {
					expected = errors.New("retryable processor failure")
				}
				called := ""
				h := roledistribution.NewHandler(testenv.NewLogger(t), roledistribution.Processors{
					Setup: func(_ context.Context, roleURN, org string) (bool, error) {
						called = "setup"
						require.Equal(t, "role:organization:"+id.String(), roleURN)
						require.Equal(t, "org-test", org)
						return false, expected
					},
					GlobalFanout: func(_ context.Context, got uuid.UUID, cursor string) error {
						called = "global"
						require.Equal(t, "org-cursor", cursor)
						require.Equal(t, id, got)
						return expected
					},
					OrganizationBootstrap: func(_ context.Context, org, cursor string) error {
						called = "bootstrap"
						require.Equal(t, "role:global:"+id.String(), cursor)
						require.Equal(t, "org-test", org)
						return expected
					},
				})
				event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
				switch target {
				case "setup":
					event.SetRoleUrn("role:organization:" + id.String())
					event.SetOrganizationId("org-test")
				case "global":
					event.SetGlobalRoleId(id.String())
					event.SetCursor("org-cursor")
				case "bootstrap":
					event.SetBootstrapOrganizationId("org-test")
					event.SetCursor("role:global:" + id.String())
				}
				err := h.HandleRoleDistributionSetupRequested(t.Context(), event, gcp.MessageMetadata{})
				if fails {
					require.ErrorIs(t, err, expected)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, target, called)
			})
		}
	}
}

func TestHandleRoleDistributionSetupRequestedPoison(t *testing.T) {
	t.Parallel()

	roleURN := "role:organization:" + uuid.NewString()
	cases := map[string]*roledistributionv1.RoleDistributionSetupRequestedV1{
		"nil":                       nil,
		"empty":                     {},
		"multiple":                  roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: &roleURN, BootstrapOrganizationId: new("org-test"), OrganizationId: new("org-test")}.Build(),
		"setup with cursor":         roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: &roleURN, OrganizationId: new("org-test"), Cursor: new("unexpected")}.Build(),
		"organization without role": roledistributionv1.RoleDistributionSetupRequestedV1_builder{OrganizationId: new("org-test")}.Build(),
		"missing organization":      roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: &roleURN}.Build(),
		"invalid role":              roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: new("bad"), OrganizationId: new("org-test")}.Build(),
		"invalid global":            roledistributionv1.RoleDistributionSetupRequestedV1_builder{GlobalRoleId: new("bad")}.Build(),
		"zero role":                 roledistributionv1.RoleDistributionSetupRequestedV1_builder{RoleUrn: new("role:organization:" + uuid.Nil.String()), OrganizationId: new("org-test")}.Build(),
		"zero global":               roledistributionv1.RoleDistributionSetupRequestedV1_builder{GlobalRoleId: new(uuid.Nil.String())}.Build(),
		"blank bootstrap":           roledistributionv1.RoleDistributionSetupRequestedV1_builder{BootstrapOrganizationId: new(" ")}.Build(),
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Unconfigured processors return errors if invoked: nil proves no dispatch.
			h := roledistribution.NewHandler(testenv.NewLogger(t), roledistribution.Processors{})
			require.NoError(t, h.HandleRoleDistributionSetupRequested(t.Context(), event, gcp.MessageMetadata{}))
		})
	}
}

func TestRoleDistributionSubscriptionPolicy(t *testing.T) {
	t.Parallel()

	message := (&roledistributionv1.RoleDistributionSetupRequestedV1{}).ProtoReflect().Descriptor()
	sub := (&roledistributionv1.RoleDistributionSetupHandler{}).ProtoReflect().Descriptor()
	policy, ok := proto.GetExtension(sub.Options(), pubsubv1.E_Subscription).(*pubsubv1.SubscriptionOptions)
	require.True(t, ok)
	require.Equal(t, string(message.FullName()), policy.GetTopic())
	require.Equal(t, 60*time.Second, policy.GetAckDeadline().AsDuration())
	require.Equal(t, 7*24*time.Hour, policy.GetRetention().AsDuration())
	require.Equal(t, 10*time.Second, policy.GetRetryPolicy().GetMinimumBackoff().AsDuration())
	require.Equal(t, 600*time.Second, policy.GetRetryPolicy().GetMaximumBackoff().AsDuration())
	require.Nil(t, policy.GetDeadLetter(), "failures retry until retention; no dead-letter topic")
	_, registered := topics.Lookup(string(message.FullName()))
	require.True(t, registered)
}
