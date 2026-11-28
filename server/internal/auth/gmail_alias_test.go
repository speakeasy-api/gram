package auth_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/users"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestGmailAliasNormalization_RejectsBlacklistedAliasSignup(t *testing.T) {
	t.Parallel()

	const (
		blockedID    = "blocked-gmail-user"
		newWorkosID  = "user_01BLOCKED_ALIAS_NEW"
		aliasEmail   = "A.l.i.c.e+puppet@googlemail.com"
		canonicalRef = "alice@gmail.com"
	)

	userInfo := &MockUserInfo{
		UserID: newWorkosID,
		Email:  aliasEmail,
	}
	ctx, instance := newTestAuthService(t, userInfo)
	usersQueries := usersRepo.New(instance.conn)

	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          blockedID,
		Email:       canonicalRef,
		DisplayName: "Blocked User",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, usersQueries.SetUserBlacklisted(ctx, usersRepo.SetUserBlacklistedParams{
		Blacklisted: true,
		ID:          blockedID,
	}))

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	_, err = instance.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{
		SkipMembershipSync: true,
	})
	require.ErrorIs(t, err, identity.ErrUserBlocked)

	// Confirm the callback redirects with the generic error and issues no session.
	ctx, stateParam := instance.stateWithNonce(ctx, t, "")
	result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
		Code:  new("mock_code"),
		State: &stateParam,
	})
	require.NoError(t, err)
	require.Empty(t, result.SessionToken)
	require.Equal(t, instance.authConfigs.SignInRedirectURL+"?signin_error=init_error", result.Location)

	// Confirm no puppet row was written.
	_, err = usersQueries.GetUser(ctx, users.UserIDFromWorkOSID(newWorkosID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestGmailAliasNormalization_RejectsExistingAliasSignup(t *testing.T) {
	t.Parallel()

	const (
		existingID  = "existing-gmail-user"
		newWorkosID = "user_01DUP_ALIAS_NEW"
		aliasEmail  = "a.l.i.c.e+work@googlemail.com"
	)

	userInfo := &MockUserInfo{
		UserID: newWorkosID,
		Email:  aliasEmail,
	}
	ctx, instance := newTestAuthService(t, userInfo)
	usersQueries := usersRepo.New(instance.conn)

	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          existingID,
		Email:       "alice@gmail.com",
		DisplayName: "Existing User",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	_, err = instance.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{
		SkipMembershipSync: true,
	})
	require.ErrorIs(t, err, identity.ErrDuplicateEmailAlias)

	ctx, stateParam := instance.stateWithNonce(ctx, t, "")
	result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
		Code:  new("mock_code"),
		State: &stateParam,
	})
	require.NoError(t, err)
	require.Empty(t, result.SessionToken)
	require.Equal(t, instance.authConfigs.SignInRedirectURL+"?signin_error=init_error", result.Location)

	_, err = usersQueries.GetUser(ctx, users.UserIDFromWorkOSID(newWorkosID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestGmailAliasNormalization_AllowsReturningUserEvenWithPreExistingDuplicate(t *testing.T) {
	t.Parallel()

	const (
		workosID      = "user_01RETURNING_ALIAS"
		returningMail = "a.l.i.c.e@googlemail.com"
	)
	returningID := users.UserIDFromWorkOSID(workosID)

	userInfo := &MockUserInfo{
		UserID: workosID,
		Email:  returningMail,
	}
	ctx, instance := newTestAuthService(t, userInfo)
	usersQueries := usersRepo.New(instance.conn)

	// Simulate two pre-existing alias rows already in the database before rollout.
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          "preexisting-primary",
		Email:       "alice@gmail.com",
		DisplayName: "Primary",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          returningID,
		Email:       returningMail,
		DisplayName: "Returning Alias",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	login, err := instance.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{
		SkipMembershipSync: true,
	})
	require.NoError(t, err)
	require.Equal(t, returningID, login.UserID)
}

func TestGmailAliasNormalization_AllowsNonGmailDotAndPlusAddresses(t *testing.T) {
	t.Parallel()

	const newWorkosID = "user_01NONGMAIL_PLUS"

	userInfo := &MockUserInfo{
		UserID: newWorkosID,
		Email:  "alice.smith+work@example.com",
	}
	ctx, instance := newTestAuthService(t, userInfo)
	usersQueries := usersRepo.New(instance.conn)

	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          "existing-nongmail-user",
		Email:       "alicesmith@example.com",
		DisplayName: "Existing Non-Gmail",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	login, err := instance.identityResolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{
		SkipMembershipSync: true,
	})
	require.NoError(t, err)
	require.Equal(t, users.UserIDFromWorkOSID(newWorkosID), login.UserID)
}

func TestGmailAliasNormalization_PreservesLeadingAndTrailingV(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID: "user_01VICTOR_TEST",
		Email:  "ictor@gmail.com",
	}
	ctx, instance := newTestAuthService(t, userInfo)
	usersQueries := usersRepo.New(instance.conn)

	// Seed users with leading 'v' local part and trailing 'v' TLD (.dev).
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          "existing-victor",
		Email:       "victor@gmail.com",
		DisplayName: "Victor",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          "existing-dev-domain",
		Email:       "dev@company.dev",
		DisplayName: "Dev",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	// ictor@gmail.com must NOT falsely collide with victor@gmail.com.
	login, err := instance.identityResolver.CompleteIDPLogin(ctx, &identity.IDPUserInfo{
		Sub:   "user_01ICTOR_NEW",
		Email: "ictor@gmail.com",
		Name:  "Ictor",
	}, identity.IDPLoginOptions{SkipMembershipSync: true})
	require.NoError(t, err)
	require.Equal(t, users.UserIDFromWorkOSID("user_01ICTOR_NEW"), login.UserID)

	// v.i.c.t.o.r+1@googlemail.com MUST collide with victor@gmail.com.
	_, err = instance.identityResolver.CompleteIDPLogin(ctx, &identity.IDPUserInfo{
		Sub:   "user_01VICTOR_ALIAS",
		Email: "v.i.c.t.o.r+1@googlemail.com",
		Name:  "Victor Alias",
	}, identity.IDPLoginOptions{SkipMembershipSync: true})
	require.ErrorIs(t, err, identity.ErrDuplicateEmailAlias)

	// dev@company.de must NOT falsely collide with dev@company.dev.
	loginDev, err := instance.identityResolver.CompleteIDPLogin(ctx, &identity.IDPUserInfo{
		Sub:   "user_01DEV_DE",
		Email: "dev@company.de",
		Name:  "Dev DE",
	}, identity.IDPLoginOptions{SkipMembershipSync: true})
	require.NoError(t, err)
	require.Equal(t, users.UserIDFromWorkOSID("user_01DEV_DE"), loginDev.UserID)
}

func TestGmailAliasNormalization_ConcurrentAliasSignupsAllowOnlyOne(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID: "user_01CONCURRENT_SEED",
		Email:  "seed@example.com",
	}
	ctx, instance := newTestAuthService(t, userInfo)

	aliases := []string{
		"alice@gmail.com",
		"a.lice@gmail.com",
		"al.ice@googlemail.com",
		"ali.ce+one@gmail.com",
		"a.l.i.c.e+two@googlemail.com",
		"ALICE+three@GMAIL.COM",
	}

	start := make(chan struct{})
	errs := make([]error, len(aliases))
	var wg sync.WaitGroup
	for i, alias := range aliases {
		wg.Add(1)
		go func(idx int, email string) {
			defer wg.Done()
			<-start
			_, errs[idx] = instance.identityResolver.CompleteIDPLogin(ctx, &identity.IDPUserInfo{
				Sub:   fmt.Sprintf("user_01CONCURRENT_%02d", idx),
				Email: email,
				Name:  "Concurrent User",
			}, identity.IDPLoginOptions{SkipMembershipSync: true})
		}(i, alias)
	}
	close(start)
	wg.Wait()

	var succeeded, duplicateRejected int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, identity.ErrDuplicateEmailAlias):
			duplicateRejected++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded, "exactly one concurrent Gmail alias sign-up must succeed")
	require.Equal(t, len(aliases)-1, duplicateRejected, "all remaining concurrent alias sign-ups must fail with ErrDuplicateEmailAlias")
}
