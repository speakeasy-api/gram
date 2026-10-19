package assistants

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This opt-in fixture needs Docker and Go, but no extra image, external service,
// paid inference, or change to the Kit binary. The internal network has no
// external route; its ai.speakeasy.com alias serves a generated trusted TLS cert.
// Run: SPEAKEASY_KIT_IMAGE_SMOKE=1 mise run test:server ./internal/assistants -run '^TestKitLocalExactImageGatewaySmoke$' -count=1
// SPEAKEASY_KIT_SMOKE_IMAGE optionally selects another already-built Kit image.
// Cleanup addresses only freshly created UUID-named resources, never shared volumes.
type kitNativeCatalogFixture struct {
	network, caFile, container string
}

type kitNativeCatalogStats struct {
	Catalog     int64 `json:"Catalog"`
	Completions int64 `json:"Completions"`
	Rejected    int64 `json:"Rejected"`
	Unexpected  int64 `json:"Unexpected"`
}

func newKitNativeCatalogFixture(t *testing.T, image string) kitNativeCatalogFixture {
	t.Helper()
	dir := t.TempDir()
	// The fixture container runs as the image's unprivileged user.
	require.NoError(t, os.Chmod(dir, 0755))
	caFile := kitSmokeCertificate(t, dir)
	source := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(source, []byte(kitNativeCatalogSource), 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	architecture, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Architecture}}", image).Output()
	require.NoError(t, err, "smoke requires an already-built local image")
	arch := strings.TrimSpace(string(architecture))
	require.Contains(t, []string{"arm64", "amd64"}, arch)
	build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(dir, "catalog"), source)
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "compile isolated catalog helper: %s", output)
	f := kitNativeCatalogFixture{network: "kit-smoke-" + uuid.NewString(), caFile: caFile}
	_, err = exec.CommandContext(ctx, "docker", "network", "create", "--internal", f.network).Output()
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, exec.CommandContext(ctx, "docker", "network", "remove", f.network).Run())
	})
	// Bind 443 as root in this throwaway fixture only; Kit itself stays UID 1000.
	output, err = exec.CommandContext(ctx, "docker", "create", "--network", f.network, "--network-alias", "ai.speakeasy.com", "--user", "0:0", "--read-only", "--cap-drop", "ALL", "--cap-add", "NET_BIND_SERVICE", "--security-opt", "no-new-privileges", "--volume", dir+":/fixture:ro", "--entrypoint", "/fixture/catalog", image).Output()
	require.NoError(t, err)
	f.container = strings.TrimSpace(string(output))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, exec.CommandContext(ctx, "docker", "container", "rm", "--force", f.container).Run())
	})
	require.NoError(t, exec.CommandContext(ctx, "docker", "start", f.container).Run())
	require.Eventually(t, func() bool {
		return exec.CommandContext(ctx, "docker", "exec", f.container, "/fixture/catalog", "stats").Run() == nil
	}, 20*time.Second, 200*time.Millisecond, "isolated TLS catalog must start")
	return f
}

func (f kitNativeCatalogFixture) stats(t *testing.T) kitNativeCatalogStats {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "docker", "exec", f.container, "/fixture/catalog", "stats").Output()
	require.NoError(t, err)
	var stats kitNativeCatalogStats
	require.NoError(t, json.Unmarshal(raw, &stats))
	return stats
}

func kitSmokeCertificate(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated smoke CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	require.NoError(t, err)
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"ai.speakeasy.com"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	require.NoError(t, err)
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
	return caFile
}

// A standard-library-only Linux binary avoids assuming curl, Python, a shell,
// or a Go installation inside the unmodified runtime image. It never logs
// request headers or bodies. The stats listener is container-loopback-only.
const kitNativeCatalogSource = `package main
import (
 "crypto/tls"
 "encoding/json"
 "io"
 "net"
 "net/http"
 "os"
 "strings"
 "sync/atomic"
 "time"
)
func main() {
 if len(os.Args) > 1 {
  client := &http.Client{Timeout: 2*time.Second}
  r, err := client.Get("http://127.0.0.1:8082")
  if err != nil { os.Exit(1) }
  defer r.Body.Close()
  if r.StatusCode != 200 { os.Exit(1) }
  if _, err := io.Copy(os.Stdout, r.Body); err != nil { os.Exit(1) }
  return
 }
 var catalog, completions, rejected, unexpected atomic.Int64
 mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.Host != "ai.speakeasy.com" || !((r.URL.Path == "/chat/models" && r.Method == "GET") || (r.URL.Path == "/chat/completions" && r.Method == "POST")) {
   unexpected.Add(1); http.NotFound(w,r); return
  }
  if r.Header.Get("Gram-Key") != "gram_local_"+strings.Repeat("a",64) || r.Header.Get("Gram-Project") != "smoke" || r.Header.Get("Authorization") != "" {
   rejected.Add(1); w.WriteHeader(401); return
  }
  if r.URL.Path == "/chat/completions" {
   completions.Add(1)
   w.Header().Set("Content-Type", "text/event-stream")
   io.WriteString(w, "data: {\"id\":\"smoke\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"openai/gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"smoke-ok\"},\"finish_reason\":null}]}\n\n")
   io.WriteString(w, "data: {\"id\":\"smoke\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"openai/gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
   return
  }
  catalog.Add(1)
  w.Header().Set("Content-Type", "application/json")
  io.WriteString(w, "{\"data\":[{\"id\":\"openai/gpt-4o-mini\",\"name\":\"Smoke model\",\"context_length\":128000,\"pricing\":{\"prompt\":\"0\",\"completion\":\"0\"},\"supported_parameters\":[\"tools\"]}]}")
 })
 cert, err := tls.LoadX509KeyPair("/fixture/cert.pem", "/fixture/key.pem")
 if err != nil { os.Exit(1) }
 listener, err := net.Listen("tcp", ":443")
 if err != nil { os.Exit(1) }
 go func() {
  server := &http.Server{Handler: mux, ReadHeaderTimeout: 5*time.Second}
  if server.Serve(tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})) != nil { os.Exit(1) }
 }()
 stats := &http.Server{Addr: "127.0.0.1:8082", ReadHeaderTimeout: 5*time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  json.NewEncoder(w).Encode(map[string]int64{"Catalog":catalog.Load(), "Completions":completions.Load(), "Rejected":rejected.Load(), "Unexpected":unexpected.Load()})
 })}
 if stats.ListenAndServe() != nil { os.Exit(1) }
}
`
