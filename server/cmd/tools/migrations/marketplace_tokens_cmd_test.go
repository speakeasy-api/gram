package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/marketplacetokenbackfill"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// testEncryptionKey is a throwaway 32-byte key, base64 encoded.
const testEncryptionKey = "dGVzdC1rZXktMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM="

func marketplaceTokensEnv(databaseURL string) func(string) string {
	return func(key string) string {
		switch key {
		case "GRAM_DATABASE_URL":
			return databaseURL
		case "GRAM_ENCRYPTION_KEY":
			return testEncryptionKey
		default:
			return ""
		}
	}
}

func TestParseMarketplaceTokensFlags(t *testing.T) {
	t.Parallel()
	getenv := marketplaceTokensEnv("postgres://user@db.internal:5432/gram?sslmode=require")
	key, err := encryption.New(testEncryptionKey)
	require.NoError(t, err)
	fingerprint := "-confirm-key-fingerprint=" + key.Fingerprint()

	cfg, err := parseMarketplaceTokensFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, marketplaceTokensDefaultBatch, cfg.batchSize)

	_, err = parseMarketplaceTokensFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseMarketplaceTokensFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseMarketplaceTokensFlags([]string{"-environment=staging", "-batch-size=10001"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	cfg, err = parseMarketplaceTokensFlags([]string{"-environment=staging", "-batch-size=10000"}, getenv)
	require.NoError(t, err)
	require.EqualValues(t, marketplaceTokensMaxBatch, cfg.batchSize)

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-key-fingerprint=0000"}, getenv)
	require.ErrorContains(t, err, "confirm-key-fingerprint", "copies sealed under the wrong key would open nowhere but here")

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", fingerprint}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", fingerprint, "-confirm-apply=marketplace-tokens", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)
}

func TestParseMarketplaceTokensFlagsRequiresTheEncryptionKeyForWrites(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://user@db.internal:5432/gram?sslmode=require"
		}
		return ""
	}

	_, err := parseMarketplaceTokensFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err, "a dry run needs no key")

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=marketplace-tokens"}, getenv)
	require.ErrorContains(t, err, "GRAM_ENCRYPTION_KEY")
}

func TestParseMarketplaceTokensFlagsRequiresTLS(t *testing.T) {
	t.Parallel()

	_, err := parseMarketplaceTokensFlags([]string{"-environment=staging"}, marketplaceTokensEnv("postgres://user@db.internal:5432/gram?sslmode=disable"))
	require.ErrorContains(t, err, "TLS", "a remote preview still sends credentials")

	_, err = parseMarketplaceTokensFlags([]string{"-environment=local"}, marketplaceTokensEnv("postgres://user@127.0.0.1:5432/gram?sslmode=disable"))
	require.NoError(t, err)

	_, err = parseMarketplaceTokensFlags([]string{"-apply", "-environment=local", "-confirm-environment=local", "-confirm-target=127.0.0.1:5432/gram", "-confirm-apply=marketplace-tokens"}, marketplaceTokensEnv("postgres://user@127.0.0.1:5432/gram?sslmode=disable"))
	require.ErrorContains(t, err, "TLS")
}

func TestParseMarketplaceTokensFlagsRejectsOtherFallbackHosts(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=marketplace-tokens"}

	_, err := parseMarketplaceTokensFlags(apply, marketplaceTokensEnv("postgres://user@db.internal:5432,other.internal:5432/gram?sslmode=require"))
	require.ErrorContains(t, err, "single host")
}

func TestFormatMarketplaceTokensReport(t *testing.T) {
	t.Parallel()

	report := marketplacetokenbackfill.Report{
		Attributed:           1,
		Moved:                2,
		Skipped:              3,
		NeedsOrganizationKey: 4,
		Counts:               marketplacetokenbackfill.Counts{Unattributed: 5, Legacy: 6, Hashed: 7},
	}
	require.Equal(t,
		"mode=apply environment=dev attributed=1 moved=2 skipped=3 needs_organization_key=4 unattributed=5 legacy=6 hashed=7\n",
		formatMarketplaceTokensReport("apply", "dev", report))
}
