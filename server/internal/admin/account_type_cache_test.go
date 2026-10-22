package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestAccountTypeCacheFailureReturnsCommittedResult(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	id := "org_tier_cache_failure"
	seedOrg(t, ctx, db, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	redisClient.AddHook(failAccountTypeCacheSetHook{})
	svc.productFeatures = productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, redisClient)
	updated, err := svc.changeAccountTypes(ctx, []string{id}, "enterprise", nil)
	require.NoError(t, err)
	require.Equal(t, []string{id}, updated)
	require.Equal(t, "enterprise", readOrgState(t, ctx, db, id).GramAccountType)
}

type failAccountTypeCacheSetHook struct{}

func (failAccountTypeCacheSetHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (failAccountTypeCacheSetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (failAccountTypeCacheSetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" {
			return errors.New("injected cache store failure")
		}
		return next(ctx, cmd)
	}
}
