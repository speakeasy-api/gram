package localaccounts

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

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestProfileValidate(t *testing.T) {
	t.Parallel()
	for _, profile := range []Profile{Enterprise, PAYG, ActiveTrial, ExpiredTrial} {
		require.NoError(t, profile.Validate())
	}
	for _, profile := range []Profile{"", "pro", "free", "Enterprise", "payg ", "unknown"} {
		require.Error(t, profile.Validate())
	}
}

func TestApplyRejectsInvalidInputBeforeAccessingDependencies(t *testing.T) {
	t.Parallel()
	for _, dryRun := range []bool{false, true} {
		result, err := Apply(t.Context(), nil, nil, "org_placeholder", "unknown", dryRun, time.Time{}, nil)
		require.ErrorContains(t, err, "profile must be")
		require.False(t, result.Committed)
		result, err = Apply(t.Context(), nil, nil, "org_placeholder", Enterprise, dryRun, time.Time{}, nil)
		require.ErrorContains(t, err, "recheck is required")
		require.False(t, result.Committed)
	}
}

func TestProfileFeaturesOnlyTouchesExplicitBundles(t *testing.T) {
	t.Parallel()
	want := append([]productfeatures.Feature{}, productfeatures.EnterpriseAccessBundle...)
	want = append(want, productfeatures.TrialRuntimeFeatures...)
	require.ElementsMatch(t, want, profileFeatures())
	require.NotContains(t, profileFeatures(), productfeatures.FeatureSkills)
	require.NotContains(t, profileFeatures(), productfeatures.FeatureHooksFailOpen)
	require.NotContains(t, profileFeatures(), productfeatures.FeatureSkillCaptureMetadataOnly)
}

type profileTestRow struct {
	mock.Mock
}

func (r *profileTestRow) Scan(dest ...any) error {
	return r.Called(dest...).Error(0)
}

type profileStateReader struct {
	mock.Mock
}

func (r *profileStateReader) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	result := r.Called(ctx, query, args)
	tag, _ := result.Get(0).(pgconn.CommandTag)
	return tag, result.Error(1)
}

func (r *profileStateReader) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	result := r.Called(ctx, query, args)
	rows, _ := result.Get(0).(pgx.Rows)
	return rows, result.Error(1)
}

func (r *profileStateReader) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	result := r.Called(ctx, query, args)
	row, _ := result.Get(0).(pgx.Row)
	return row
}

func TestReadStateOptionalMarkerAndReadFailures(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("database unavailable")
	for _, tt := range []struct {
		name       string
		exists     bool
		stateErr   error
		schemaErr  error
		markerErr  error
		wantErr    bool
		wantMarker bool
	}{
		{name: "schema absent"},
		{name: "marker absent", exists: true, markerErr: pgx.ErrNoRows},
		{name: "marker present", exists: true, wantMarker: true},
		{name: "organization absent", stateErr: pgx.ErrNoRows, wantErr: true},
		{name: "organization read failure", stateErr: sentinel, wantErr: true},
		{name: "schema read failure", schemaErr: sentinel, wantErr: true},
		{name: "marker read failure", exists: true, markerErr: sentinel, wantErr: true},
	} {
		t.Log(tt.name)
		reader := &profileStateReader{}
		reader.Test(t)
		t.Cleanup(func() { require.True(t, reader.AssertExpectations(t)) })
		stateRow := &profileTestRow{}
		stateRow.On("Scan", mock.AnythingOfType("*string"), mock.AnythingOfType("*bool"), mock.AnythingOfType("*[]uint8"), mock.AnythingOfType("*[]string")).Run(func(args mock.Arguments) {
			value0, ok := args.Get(0).(*string)
			require.True(t, ok)
			*value0 = "enterprise"
			value1, ok := args.Get(1).(*bool)
			require.True(t, ok)
			*value1 = true
			value2, ok := args.Get(2).(*[]byte)
			require.True(t, ok)
			*value2 = []byte("null")
			value3, ok := args.Get(3).(*[]string)
			require.True(t, ok)
			*value3 = []string{"logs"}
		}).Return(tt.stateErr).Once()
		reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
			return strings.HasPrefix(query, "-- name: ReadAccountState :one")
		}), []any{"org_placeholder"}).Return(stateRow).Once()
		t.Cleanup(func() { require.True(t, stateRow.AssertExpectations(t)) })
		if tt.stateErr == nil {
			schemaRow := &profileTestRow{}
			schemaRow.On("Scan", mock.AnythingOfType("*bool")).Run(func(args mock.Arguments) {
				value0, ok := args.Get(0).(*bool)
				require.True(t, ok)
				*value0 = tt.exists
			}).Return(tt.schemaErr).Once()
			reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
				return strings.HasPrefix(query, "-- name: AccountProfilesExist :one")
			}), []any(nil)).Return(schemaRow).Once()
			t.Cleanup(func() { require.True(t, schemaRow.AssertExpectations(t)) })
		}
		if tt.stateErr == nil && tt.schemaErr == nil && tt.exists {
			markerRow := &profileTestRow{}
			markerRow.On("Scan", mock.AnythingOfType("*string"), mock.AnythingOfType("*pgtype.Timestamptz")).Run(func(args mock.Arguments) {
				value0, ok := args.Get(0).(*string)
				require.True(t, ok)
				*value0 = string(Enterprise)
				value1, ok := args.Get(1).(*pgtype.Timestamptz)
				require.True(t, ok)
				*value1 = pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true}
			}).Return(tt.markerErr).Once()
			reader.On("QueryRow", mock.Anything, mock.MatchedBy(func(query string) bool {
				return strings.HasPrefix(query, "-- name: GetAccountProfile :one")
			}), []any{pgtype.Text{String: "org_placeholder", Valid: true}}).Return(markerRow).Once()
			t.Cleanup(func() { require.True(t, markerRow.AssertExpectations(t)) })
		}
		state, err := readState(t.Context(), reader, "org_placeholder")
		if tt.wantErr {
			wantErr := tt.stateErr
			if wantErr == nil {
				wantErr = tt.schemaErr
			}
			if wantErr == nil {
				wantErr = tt.markerErr
			}
			require.ErrorIs(t, err, wantErr)
		} else {
			require.NoError(t, err)
			require.Equal(t, "enterprise", state.AccountType)
			require.True(t, state.Whitelisted)
			require.JSONEq(t, "null", string(state.Trial))
			require.Equal(t, []string{"logs"}, state.Features)
			if tt.wantMarker {
				require.Equal(t, Enterprise, state.Profile)
				require.NotNil(t, state.Anchor)
			} else {
				require.Empty(t, state.Profile)
				require.Nil(t, state.Anchor)
			}
		}
	}
}
