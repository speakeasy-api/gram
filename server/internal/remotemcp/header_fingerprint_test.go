package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHeaderConfigurationFingerprintUsesStoredCiphertext(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	first := createTestServer(t, ctx, ti)
	second := createTestServer(t, ctx, ti)
	for _, id := range []string{first.ID, second.ID} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(id, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
			p.IsSecret = new(true)
			p.Value = new("synthetic-low-entropy-value")
		}))
		require.NoError(t, err)
	}
	// No encryption client: computing evidence must not need decryption.
	headers := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, nil)
	firstHash, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(first.ID))
	require.NoError(t, err)
	require.NotEmpty(t, firstHash)
	again, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(first.ID))
	require.NoError(t, err)
	require.Equal(t, firstHash, again)
	secondHash, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(second.ID))
	require.NoError(t, err)
	require.NotEqual(t, firstHash, secondHash, "equal plaintext encrypted with different nonces must not have equal fingerprints")
}

func TestHeaderConfigurationFingerprintDetectsChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mutate  func(*repo.UpdateServerHeaderParams)
		changed bool
	}{
		{name: "secret value", mutate: func(p *repo.UpdateServerHeaderParams) {
			p.SetValue = true
			p.Value = conv.ToPGText("rotated-synthetic-secret")
		}, changed: true},
		{name: "required", mutate: func(p *repo.UpdateServerHeaderParams) { p.IsRequired = true }, changed: true},
		{name: "name", mutate: func(p *repo.UpdateServerHeaderParams) { p.Name = "X-Other-Key" }, changed: true},
		{name: "description", mutate: func(p *repo.UpdateServerHeaderParams) { p.Description = conv.ToPGText("Display-only change") }, changed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			server := createTestServer(t, ctx, ti)
			header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
				p.IsSecret = new(true)
				p.Value = new("original-synthetic-secret")
			}))
			require.NoError(t, err)
			headers := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc)
			before, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(server.ID))
			require.NoError(t, err)
			params := repo.UpdateServerHeaderParams{ID: uuid.MustParse(header.ID), ProjectID: uuid.MustParse(server.ProjectID), Name: header.Name, IsSecret: true}
			tc.mutate(&params)
			_, err = headers.UpdateServerHeader(ctx, params)
			require.NoError(t, err)
			after, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(server.ID))
			require.NoError(t, err)
			if tc.changed {
				require.NotEqual(t, before, after)
			} else {
				require.Equal(t, before, after)
			}
		})
	}
}

func TestHeaderConfigurationFingerprintDetectsRequestHeaderChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Original-Key")
	}))
	require.NoError(t, err)

	headers := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc)
	before, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(server.ID))
	require.NoError(t, err)

	_, err = headers.UpdateServerHeader(ctx, repo.UpdateServerHeaderParams{
		ID: uuid.MustParse(header.ID), ProjectID: uuid.MustParse(server.ProjectID), Name: header.Name,
		ValueFromRequestHeader: conv.ToPGText("X-Replacement-Key"),
	})
	require.NoError(t, err)

	after, err := headers.ConfigurationFingerprint(ctx, uuid.MustParse(server.ID))
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}
