package billing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type localProfileRow struct {
	mock.Mock
}

func (r *localProfileRow) Scan(dest ...any) error {
	return r.Called(dest...).Error(0)
}

type localProfileReader struct {
	mock.Mock
}

func (r *localProfileReader) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	result := r.Called(ctx, query, args)
	tag, _ := result.Get(0).(pgconn.CommandTag)
	return tag, result.Error(1)
}

func (r *localProfileReader) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	result := r.Called(ctx, query, args)
	rows, _ := result.Get(0).(pgx.Rows)
	return rows, result.Error(1)
}

func (r *localProfileReader) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	result := r.Called(ctx, query, args)
	row, _ := result.Get(0).(pgx.Row)
	return row
}

func TestStubLocalAccountProfiles(t *testing.T) {
	t.Parallel()
	validAnchor := pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	for _, tt := range []struct {
		name         string
		profile      string
		anchor       pgtype.Timestamptz
		missingTable bool
		checkErr     error
		readErr      error
		tier         billing.Tier
		active       bool
		wantErr      bool
		trialActive  bool
		trialErr     error
	}{
		{name: "missing table", missingTable: true, tier: billing.TierPro, active: true},
		{name: "missing profile", readErr: pgx.ErrNoRows, tier: billing.TierPro, active: true},
		{name: "expired trial", profile: "expired-trial", anchor: validAnchor, tier: billing.TierBase},
		{name: "enterprise", profile: "enterprise", anchor: validAnchor, tier: billing.TierEnterprise, active: true},
		{name: "active trial", profile: "active-trial", anchor: validAnchor, tier: billing.TierEnterprise, active: true, trialActive: true},
		{name: "elapsed or demoted active fixture", profile: "active-trial", anchor: validAnchor, tier: billing.TierBase},
		{name: "missing active lifecycle", profile: "active-trial", anchor: validAnchor, trialErr: pgx.ErrNoRows, wantErr: true},
		{name: "active lifecycle read failure", profile: "active-trial", anchor: validAnchor, trialErr: errors.New("unavailable"), wantErr: true},
		{name: "payg", profile: "payg", anchor: validAnchor, tier: billing.TierPayg, active: true},
		{name: "unknown profile", profile: "unknown", anchor: validAnchor, wantErr: true},
		{name: "empty profile", anchor: validAnchor, wantErr: true},
		{name: "null anchor", profile: "enterprise", wantErr: true},
		{name: "infinite anchor", profile: "enterprise", anchor: pgtype.Timestamptz{Valid: true, InfinityModifier: pgtype.Infinity}, wantErr: true},
		{name: "zero anchor", profile: "enterprise", anchor: pgtype.Timestamptz{Valid: true}, wantErr: true},
		{name: "existence read error", checkErr: errors.New("unavailable"), wantErr: true},
		{name: "profile read error", readErr: errors.New("unavailable"), wantErr: true},
		{name: "malformed row", readErr: errors.New("cannot scan NULL into string"), wantErr: true},
		{name: "table disappeared", readErr: errors.New("relation does not exist"), wantErr: true},
	} {

		t.Log(tt.name)

		reader := &localProfileReader{}
		reader.Test(t)
		t.Cleanup(func() { require.True(t, reader.AssertExpectations(t)) })
		schemaRow := &localProfileRow{}
		schemaRow.On("Scan", mock.AnythingOfType("*bool")).Run(func(args mock.Arguments) {
			value0, ok := args.Get(0).(*bool)
			require.True(t, ok)
			*value0 = !tt.missingTable
		}).Return(tt.checkErr).Once()
		reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
			return strings.HasPrefix(query, "-- name: AccountProfilesExist :one")
		}), []any(nil)).Return(schemaRow).Once()
		t.Cleanup(func() { require.True(t, schemaRow.AssertExpectations(t)) })
		if !tt.missingTable && tt.checkErr == nil {
			markerRow := &localProfileRow{}
			markerRow.On("Scan", mock.AnythingOfType("*string"), mock.AnythingOfType("*pgtype.Timestamptz")).Run(func(args mock.Arguments) {
				value0, ok := args.Get(0).(*string)
				require.True(t, ok)
				*value0 = tt.profile
				value1, ok := args.Get(1).(*pgtype.Timestamptz)
				require.True(t, ok)
				*value1 = tt.anchor
			}).Return(tt.readErr).Once()
			reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
				return strings.HasPrefix(query, "-- name: GetAccountProfile :one")
			}), []any{pgtype.Text{String: "org_placeholder", Valid: true}}).Return(markerRow).Once()
			t.Cleanup(func() { require.True(t, markerRow.AssertExpectations(t)) })
			if tt.profile == "active-trial" && tt.readErr == nil && tt.anchor.Valid {
				trialRow := &localProfileRow{}
				trialRow.On("Scan", mock.AnythingOfType("*pgtype.Bool")).Run(func(args mock.Arguments) {
					value0, ok := args.Get(0).(*pgtype.Bool)
					require.True(t, ok)
					*value0 = pgtype.Bool{Bool: tt.trialActive, Valid: true}
				}).Return(tt.trialErr).Once()
				reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
					return strings.HasPrefix(query, "-- name: AccountTrialActive :one")
				}), []any{"org_placeholder"}).Return(trialRow).Once()
				t.Cleanup(func() { require.True(t, trialRow.AssertExpectations(t)) })
			}
		}
		client := billing.NewStubClientWithLocalProfiles(testenv.NewLogger(t), testenv.NewTracerProvider(t), reader)
		tier, active, err := client.GetCustomerTier(t.Context(), "org_placeholder")
		if tt.wantErr {
			require.Error(t, err)
			require.Nil(t, tier)
			require.False(t, active)
		} else {
			require.NoError(t, err)
			require.Equal(t, &tt.tier, tier)
			require.Equal(t, tt.active, active)
		}

	}
}

func TestStubWithoutLocalProfilesUnchanged(t *testing.T) {
	t.Parallel()
	client := billing.NewStubClient(testenv.NewLogger(t), testenv.NewTracerProvider(t))
	tier, active, err := client.GetCustomerTier(t.Context(), "org_placeholder")
	require.NoError(t, err)
	require.Equal(t, new(billing.TierPro), tier)
	require.True(t, active)
}
