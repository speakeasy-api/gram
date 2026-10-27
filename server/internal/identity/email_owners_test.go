package identity_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/identity"
	organizationsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// A role filter hands the resolver every member address at once, so the batch
// must apply the single-address rule to each address independently: one
// directory owner wins, a case-variant pair is ambiguous, a linked account
// resolves only to a single owner, and an unknown address resolves to nobody.
func TestEmailOwnerUserIDs_AppliesOwnershipRulePerAddress(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "identity")
	require.NoError(t, err)
	ctx := t.Context()
	orgID := "org_" + uuid.NewString()

	_, err = organizationsRepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsRepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     orgID,
		Slug:     orgID,
		WorkosID: conv.ToPGText("workos_" + orgID),
	})
	require.NoError(t, err)

	seedMember := func(email string) string {
		t.Helper()
		userID := uuid.NewString()
		_, err := usersRepo.New(conn).UpsertUser(ctx, usersRepo.UpsertUserParams{
			ID:          userID,
			Email:       email,
			DisplayName: email,
			PhotoUrl:    conv.PtrToPGText(nil),
			Admin:       false,
		})
		require.NoError(t, err)
		_, err = organizationsRepo.New(conn).UpsertOrganizationUserRelationship(ctx, organizationsRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: orgID,
			UserID:         conv.ToPGText(userID),
		})
		require.NoError(t, err)
		return userID
	}
	linkAccount := func(userID, email string) {
		t.Helper()
		_, err := hooksRepo.New(conn).UpsertUserAccount(ctx, hooksRepo.UpsertUserAccountParams{
			OrganizationID:      orgID,
			Provider:            "anthropic",
			ExternalAccountUuid: uuid.NewString(),
			UserID:              conv.ToPGText(userID),
			ExternalOrgID:       conv.PtrToPGText(nil),
			ExternalAccountID:   conv.PtrToPGText(nil),
			Email:               conv.ToPGText(email),
			AccountType:         conv.ToPGText("personal"),
		})
		require.NoError(t, err)
	}

	owner := seedMember("owner@example.com")
	seedMember("twin@example.com")
	seedMember("Twin@example.com")
	linkedOwner := seedMember("linked-owner@example.com")
	linkAccount(linkedOwner, "personal@example.com")
	sharerA := seedMember("sharer-a@example.com")
	sharerB := seedMember("sharer-b@example.com")
	linkAccount(sharerA, "shared@example.com")
	linkAccount(sharerB, "shared@example.com")

	resolver := identity.NewResolver(testenv.NewLogger(t), conn)
	owners := resolver.EmailOwnerUserIDs(ctx, orgID, []string{
		"Owner@Example.com",
		"twin@example.com",
		"personal@example.com",
		"shared@example.com",
		"nobody@example.com",
	})

	require.Equal(t, map[string]string{
		"owner@example.com":    owner,
		"personal@example.com": linkedOwner,
	}, owners)
}
