package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercises the pinned Kit gateway and real nginx transport, without inference.
//
//nolint:paralleltest // Opt-in image smoke serializes container-engine work.
func TestKitGKETLSProxyImageSmoke(t *testing.T) {
	if os.Getenv("GRAM_KIT_TLS_PROXY_SMOKE") != "1" {
		t.Skip("set GRAM_KIT_TLS_PROXY_SMOKE=1 to exercise the pinned Kit TLS sidecar") //nolint:forbidigo // Requires a local image and container engine.
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	docker := func(stdin io.Reader, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = stdin
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "docker %s: %s", args[0], out)
		return strings.TrimSpace(string(out))
	}
	kit := "kit-tls-smoke-" + uuid.NewString()
	proxy := kit + "-proxy"
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for _, name := range []string{proxy, kit} {
			// Never print container logs: credentials must remain private.
			_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
		}
	})
	// Copy into stopped containers, preserving private file ownership and modes.
	provision := func(container string, dirs []string, files map[string][]byte) {
		t.Helper()
		var archive bytes.Buffer
		tw := tar.NewWriter(&archive)
		for _, dir := range dirs {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0700, Uid: 1000, Gid: 1000}))
		}
		for name, data := range files {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Uid: 1000, Gid: 1000, Size: int64(len(data))}))
			_, err := tw.Write(data)
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		docker(&archive, "cp", "-a", "-", container+":/")
	}
	const serverName = "runtime.kit.internal"
	token := uuid.NewString()
	cert, key, err := newKitGKETLSCertificate(serverName)
	require.NoError(t, err)
	docker(nil, "create", "--name", kit, "--user", "1000:1000", "--env", "HOME=/home/kit", "--publish", "127.0.0.1::8081", "--entrypoint", "/bin/sh", KitIntegrationImage, "-c", "mkdir -p /home/kit/workspace && exec /usr/local/bin/kit gateway --listen 127.0.0.1:17766 --credential-file /home/kit/.kit/credentials/gateway-token --project /home/kit/workspace")
	provision(kit, []string{"home/kit/.kit/", "home/kit/.kit/credentials/"}, map[string][]byte{"home/kit/.kit/credentials/gateway-token": []byte(token)})
	docker(nil, "start", kit)
	docker(nil, "create", "--name", proxy, "--user", "1000:1000", "--network", "container:"+kit, "--entrypoint", "nginx", kitGKETLSProxyImage, "-c", "/tls/nginx.conf", "-g", "daemon off;")
	provision(proxy, []string{"tls/"}, map[string][]byte{"tls/tls.crt": cert, "tls/tls.key": key, "tls/nginx.conf": []byte(kitGKETLSProxyConfig(8081))})
	docker(nil, "start", proxy)
	address := docker(nil, "port", kit, "8081/tcp")
	require.True(t, strings.HasPrefix(address, "127.0.0.1:"))
	endpoint := "https://" + address + "/acp/v2"
	config, err := kitGKETLSConfig(cert, serverName)
	require.NoError(t, err)
	clientFor := func(config *tls.Config) *guardian.HTTPClient {
		tr := &http.Transport{TLSClientConfig: config, TLSHandshakeTimeout: 5 * time.Second}
		t.Cleanup(tr.CloseIdleConnections)
		return &guardian.HTTPClient{Transport: tr, Timeout: 5 * time.Second}
	}
	hc := clientFor(config)
	params := json.RawMessage(`{"protocolVersion":2,"info":{"name":"tls-sidecar-smoke","version":"1"},"capabilities":{}}`)
	var client *kitacp.Client
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		client, _, err = kitacp.Dial(ctx, endpoint, token, hc, params)
		require.NoError(collect, err, "authenticated ACP initialize through TLS sidecar")
	}, 30*time.Second, 100*time.Millisecond)
	defer func() { _ = client.CloseTransport(context.Background()) }()
	// A real handshake must present the generated runtime certificate.
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", address, config)
	require.NoError(t, err)
	require.Equal(t, serverName, conn.ConnectionState().PeerCertificates[0].DNSNames[0])
	require.NoError(t, conn.Close())
	otherCert, _, err := newKitGKETLSCertificate(serverName)
	require.NoError(t, err)
	wrongCert, err := kitGKETLSConfig(otherCert, serverName)
	require.NoError(t, err)
	wrongName := config.Clone()
	wrongName.ServerName = "other-runtime.kit.internal"
	for _, check := range []struct {
		name, endpoint, token string
		config                *tls.Config
	}{
		{"plaintext", "http://" + address + "/acp/v2", token, config},
		{"wrong certificate", endpoint, token, wrongCert},
		{"wrong hostname", endpoint, token, wrongName},
		{"wrong bearer", endpoint, uuid.NewString(), config},
	} {
		rejected, _, err := kitacp.Dial(ctx, check.endpoint, check.token, clientFor(check.config), params)
		if rejected != nil {
			_ = rejected.CloseTransport(context.Background())
		}
		require.Error(t, err, check.name)
	}
}
