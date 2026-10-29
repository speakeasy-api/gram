package authclients

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

type verifyGitHubAppFunc func(context.Context, string, int64, int64, string) error

func (f verifyGitHubAppFunc) Verify(ctx context.Context, instance string, app, installation int64, key string) error {
	return f(ctx, instance, app, installation, key)
}

func SetGitHubAppTestVerifier(s *Service, enc *encryption.Client, verify func(context.Context, string, int64, int64, string) error) {
	s.enc, s.githubApps = enc, verifyGitHubAppFunc(verify)
}
