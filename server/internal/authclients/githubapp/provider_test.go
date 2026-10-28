package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type fixtureRow struct {
	values []any
	err    error
}

func (r fixtureRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

type fixtureDB struct {
	config   repo.McpAuthClientGithubApp
	target   string
	disabled bool
}

func (*fixtureDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected write")
}
func (*fixtureDB) Query(context.Context, string, ...any) (pgx.Rows, error) { panic("unexpected query") }
func (d *fixtureDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c := d.config
	if strings.Contains(sql, "GetGitHubAppCredential") {
		if d.disabled || args[0] != c.AuthClientID || args[1] != c.OrganizationID {
			return fixtureRow{err: pgx.ErrNoRows}
		}
		return fixtureRow{values: []any{c.AuthClientID, c.OrganizationID, c.InstanceUrl, c.AppID, c.InstallationID, c.PrivateKeyEncrypted, c.UpstreamOrigin, c.CredentialVersion}}
	}
	if strings.Contains(sql, "GetGitHubAppServerURL") {
		if args[0] != c.OrganizationID {
			return fixtureRow{err: pgx.ErrNoRows}
		}
		return fixtureRow{values: []any{d.target}}
	}
	panic("unexpected query")
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCredentialLifecycle(t *testing.T) {
	t.Parallel()
	enc, err := encryption.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	secret := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	encrypted, err := enc.Encrypt(secret)
	require.NoError(t, err)
	db := &fixtureDB{config: repo.McpAuthClientGithubApp{AuthClientID: uuid.New(), OrganizationID: "org_fixture", InstanceUrl: "https://git.example.com", AppID: 123, InstallationID: 456, PrivateKeyEncrypted: encrypted, UpstreamOrigin: "https://mcp.example.com", CredentialVersion: uuid.New()}, target: "https://mcp.example.com/mcp"}
	provider := New(db, enc, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))
	var calls atomic.Int32
	provider.http.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, "https://git.example.com/api/v3/app/installations/456/access_tokens", req.URL.String())
		body, err := json.Marshal(map[string]any{"token": fmt.Sprintf("TEST_TOKEN_%d", calls.Load()), "expires_at": time.Now().Add(time.Hour)})
		require.NoError(t, err)
		return &http.Response{StatusCode: 201, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	get := func() (string, error) {
		c, err := provider.Credential(t.Context(), db.config.OrganizationID, db.config.AuthClientID, uuid.New())
		return c.Value(), err
	}
	first, err := get()
	require.NoError(t, err)
	second, err := get()
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.EqualValues(t, 1, calls.Load())
	db.config.CredentialVersion = uuid.New()
	rotated, err := get()
	require.NoError(t, err)
	require.NotEqual(t, first, rotated)
	require.EqualValues(t, 2, calls.Load())
	db.disabled = true
	_, err = get()
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load())
	db.disabled = false
	_, err = provider.Credential(t.Context(), "org_other", db.config.AuthClientID, uuid.New())
	require.Error(t, err)
	db.target = "https://attacker.example.com/mcp"
	_, err = get()
	require.ErrorContains(t, err, "allowed origin")
	require.EqualValues(t, 2, calls.Load())
	db.target = "https://mcp.example.com/mcp"
	credential, err := provider.Credential(t.Context(), db.config.OrganizationID, db.config.AuthClientID, uuid.New())
	require.NoError(t, err)
	require.NotContains(t, fmt.Sprintf("%v", credential), rotated)
	raw, err := json.Marshal(credential)
	require.NoError(t, err)
	require.NotContains(t, string(raw), rotated)
	require.Less(t, time.Until(credential.ExpiresAt()), time.Hour)
	changed, err := credential.Forget(t.Context())
	require.NoError(t, err)
	require.False(t, changed, "fresh rejected tokens must not trigger mint storms")
}

func TestOriginAndRedirects(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"http://mcp.example.com", "https://a:b@mcp.example.com", "https://mcp.example.com/path", "https://mcp.example.com?", "https://mcp.example.com#fragment"} {
		_, err := Origin(value)
		require.Error(t, err)
	}
	provider := New(nil, nil, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))
	req, err := http.NewRequest(http.MethodGet, "https://other.example.com", nil)
	require.NoError(t, err)
	require.ErrorIs(t, provider.http.CheckRedirect(req, nil), http.ErrUseLastResponse)
}
