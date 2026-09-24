package gram

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

func newPromptPolicyCascade(judge *ppopenrouter.Judge, policy *guardian.Policy, provisioner openrouter.Provisioner, flags feature.Provider, db repo.DBTX) promptpolicy.Evaluator {
	prefilter := typesafe.New(policy.PooledClient(), func(ctx context.Context, orgID string) (string, error) {
		key, err := provisioner.ProvisionAPIKey(ctx, orgID, openrouter.KeyTypeInternal)
		if err != nil {
			return "", fmt.Errorf("provision policy prefilter key: %w", err)
		}
		return key, nil
	})
	return ppopenrouter.NewCascade(judge, prefilter, flags, db).Evaluate
}
