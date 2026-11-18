//go:build tailnetgateway_issuance

package issuancetest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/letsencrypt/pebble/v2/ca"
	pebbledb "github.com/letsencrypt/pebble/v2/db"
	"github.com/letsencrypt/pebble/v2/va"
	"github.com/letsencrypt/pebble/v2/wfe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tsnet"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const (
	magicDNSDomain = "tail-scale.ts.net"
	serviceName    = "svc:issuance"
	serviceFQDN    = "issuance." + magicDNSDomain
	gatewayTag     = "tag:speakeasy-gateway"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	// tsnet keeps certificates in the state store, shared by every slot,
	// only in Kubernetes. Set before anything asks, since tsnet caches it.
	mustSetenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")
	mustSetenv("KUBERNETES_SERVICE_PORT", "443")
	// Pebble accepts every challenge at once, never rejects a nonce and
	// never reuses an authorization, so every order runs the same steps and
	// the only thing a test waits on is the gate it controls.
	mustSetenv("PEBBLE_VA_ALWAYS_VALID", "1")
	mustSetenv("PEBBLE_VA_NOSLEEP", "1")
	mustSetenv("PEBBLE_WFE_NONCEREJECT", "0")
	mustSetenv("PEBBLE_AUTHZREUSE", "0")

	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func mustSetenv(key, value string) {
	if err := os.Setenv(key, value); err != nil {
		log.Fatalf("set %s: %v", key, err)
	}
}

// acmeServer is Pebble behind a gate: new orders block until the gate
// opens, so a test can hold the first issuance open and see what else asks.
type acmeServer struct {
	orders    atomic.Int32
	finalized atomic.Int32
	gate      chan struct{}
	openOnce  sync.Once

	// mu guards replaces, the certificate each new order names as the one
	// it replaces, "" for none.
	mu       sync.Mutex
	replaces []string
}

// orderReplaces reads the replaces field from a new-order request, which is a
// JWS whose payload is base64url JSON.
func orderReplaces(body []byte) string {
	var jws struct {
		Payload string `json:"payload"`
	}
	if json.Unmarshal(body, &jws) != nil {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err != nil {
		return ""
	}
	var order struct {
		Replaces string `json:"replaces"`
	}
	_ = json.Unmarshal(payload, &order)
	return order.Replaces
}

func (s *acmeServer) orderReplacements() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.replaces...)
}

func startACME(t *testing.T) *acmeServer {
	t.Helper()
	discard := log.New(io.Discard, "", 0)
	store := pebbledb.NewMemoryStore()
	authority := ca.New(discard, store, "", "ecdsa", 0, 1, map[string]ca.Profile{"default": {Description: "default", ValidityPeriod: 0}})
	validator := va.New(discard, 80, 443, false, "", store)
	frontend := wfe.New(discard, store, validator, authority, nil, false, false, 0, 0)
	handler := frontend.Handler()

	server := &acmeServer{gate: make(chan struct{})}
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/order-plz":
			server.orders.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			server.mu.Lock()
			server.replaces = append(server.replaces, orderReplaces(body))
			server.mu.Unlock()
			select {
			case <-server.gate:
			case <-r.Context().Done():
				return
			}
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/finalize-order/"):
			server.finalized.Add(1)
			// Let's Encrypt names the order in Location when finalizing, and
			// Go's ACME client polls it; Pebble leaves it out.
			w.Header().Set("Location", "https://"+r.Host+"/my-order/"+strings.TrimPrefix(r.URL.Path, "/finalize-order/"))
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	t.Cleanup(server.open)
	// Pebble only accepts requests signed for https URLs. tailscaled's ACME
	// client uses the default transport, so trust the test server there.
	roots := x509.NewCertPool()
	roots.AddCert(httpServer.Certificate())
	transport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	mustSetenv("TS_DEBUG_ACME_DIRECTORY_URL", httpServer.URL+"/dir")
	return server
}

func (s *acmeServer) open() { s.openOnce.Do(func() { close(s.gate) }) }

func startControl(t *testing.T) *testcontrol.Server {
	t.Helper()
	netns.SetEnabled(false)
	t.Cleanup(func() { netns.SetEnabled(true) })

	control := &testcontrol.Server{
		DERPMap:        integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true, CertDomains: []string{serviceFQDN}},
		MagicDNSDomain: magicDNSDomain,
		TagOwners:      map[string][]string{gatewayTag: nil},
		Logf:           logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	return control
}

// approveWhenSeen gives each gateway machine, once it registers, the
// service-host capability for its own address of the service, as an
// auto-approving policy would. Real hosts share one service address; each
// slot gets its own here so the client can reach a chosen slot.
func approveWhenSeen(t *testing.T, control *testcontrol.Server, addrs map[string]netip.Addr) {
	t.Helper()
	ctx := t.Context()
	go func() {
		approved := map[string]bool{}
		for len(approved) < len(addrs) {
			for _, node := range control.AllNodes() {
				hostname := node.Hostinfo.Hostname()
				addr, ok := addrs[hostname]
				if !ok || approved[hostname] || len(node.Tags) == 0 {
					continue
				}
				mapping, err := json.Marshal(tailcfg.ServiceIPMappings{serviceName: {addr}})
				if err != nil {
					panic(err)
				}
				caps := make(tailcfg.NodeCapMap, 1)
				caps[nodecap.ServiceHost] = []tailcfg.RawMessage{tailcfg.RawMessage(mapping)}
				control.SetNodeCapMap(node.Key, caps)
				control.SetSubnetRoutes(node.Key, []netip.Prefix{netip.PrefixFrom(addr, 32)})
				approved[hostname] = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

func seedIngress(t *testing.T, db *pgxpool.Pool) (uuid.UUID, string) {
	t.Helper()
	ctx := t.Context()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Tailnet Gateway Issuance Test", Slug: "issuance-" + uuid.NewString()[:8], WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	id := uuid.New()
	require.NoError(t, testrepo.New(db).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: id, OrganizationID: orgID, DnsName: pgtype.Text{String: serviceFQDN, Valid: true},
	}))
	_, err = repo.New(db).MarkNetworkIngressRuntime(ctx, repo.MarkNetworkIngressRuntimeParams{Runtime: pgtype.Text{String: "gateway", Valid: true}, ID: id, OrganizationID: orgID})
	require.NoError(t, err)
	return id, orgID
}

// startClient joins an ordinary tailnet client that accepts the routes the
// slots advertise for their service addresses.
func startClient(t *testing.T, controlURL string) *tsnet.Server {
	t.Helper()
	client := &tsnet.Server{Dir: t.TempDir(), Hostname: "client", ControlURL: controlURL, Ephemeral: true, Logf: logger.Discard}
	t.Cleanup(func() { _ = client.Close() })
	_, err := client.Up(t.Context())
	require.NoError(t, err)
	lc, err := client.LocalClient()
	require.NoError(t, err)
	_, err = lc.EditPrefs(t.Context(), &ipn.MaskedPrefs{RouteAllSet: true, Prefs: ipn.Prefs{RouteAll: true}})
	require.NoError(t, err)
	return client
}

// peerEcho answers with the tailnet peer the gateway resolves for the
// request, as its handler does before asking WhoIs.
func peerEcho() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := tailnetgateway.PeerAddr(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, peer.String())
	})
}

// request sends one HTTPS request to a slot's service address over the
// tailnet and returns the negotiated protocol and the response body.
func request(ctx context.Context, client *tsnet.Server, addr netip.Addr) (string, string, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return client.Dial(ctx, "tcp", net.JoinHostPort(addr.String(), "443"))
		},
		// Pebble's root is not trusted; the name is what matters here.
		TLSClientConfig:   &tls.Config{ServerName: serviceFQDN, InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+serviceFQDN+"/", nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	res, err := transport.RoundTrip(req)
	if err != nil {
		return "", "", fmt.Errorf("round trip: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return res.Proto, "", fmt.Errorf("status %d: %s", res.StatusCode, body)
	}
	return res.Proto, string(body), nil
}

// heartbeat polls a slot's status as the gateway does, which is what starts
// a certificate request when the slot was approved after it started.
func heartbeat(ctx context.Context, node tailnetgateway.Node) {
	for {
		_, _ = node.Status(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// handshake opens a TLS connection to one slot's service address, as an MCP
// client would, and reports whether the handshake completed.
func handshake(ctx context.Context, client *tsnet.Server, addr netip.Addr) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := client.Dial(ctx, "tcp", net.JoinHostPort(addr.String(), "443"))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = raw.Close() }()
	// Pebble's root is not trusted; the name is what matters here.
	conn := tls.Client(raw, &tls.Config{ServerName: serviceFQDN, InsecureSkipVerify: true})
	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	return nil
}

// TestOneCertificatePerConnection starts two slots of one connection while
// the first issuance is held open, sends a client to each slot, and checks
// that the connection still ends up with exactly one certificate. A slot that
// asks Let's Encrypt for a certificate outside the issuance lease, as tsnet
// does when it terminates TLS for a client and has no certificate stored,
// spends an extra certificate from the customer's tailnet allowance.
//
// It sets process-wide state, the ACME directory and the default transport's
// roots, so it does not run in parallel.
func TestOneCertificatePerConnection(t *testing.T) { //nolint:paralleltest // sets process-wide state
	ctx := t.Context()
	acme := startACME(t)
	control := startControl(t)
	controlURL := control.HTTPTestServer.URL

	db, err := infra.CloneTestDatabase(t, "tailnetissuance")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db)
	encryption := orgencryptiontest.New(t, db)
	require.NoError(t, encryption.Provisioner.EnsureUseCase(ctx, orgID, tailnetgateway.EncryptionUseCase, orgencryption.SystemActor()))

	addrs := map[string]netip.Addr{
		"speakeasy-gw-0": netip.MustParseAddr("100.100.0.10"),
		"speakeasy-gw-1": netip.MustParseAddr("100.100.0.11"),
	}
	approveWhenSeen(t, control, addrs)
	client := startClient(t, controlURL)

	runtime := tailnetgateway.NewTsnetRuntime(tailnetgateway.WithLogger(testenv.NewLogger(t)))
	nodes := make([]tailnetgateway.Node, len(addrs))
	starts := sync.WaitGroup{}
	for slot := range int32(len(addrs)) {
		store, err := tailnetgateway.NewPostgresStateStore(db, encryption.Service, ingressID, orgID, slot)
		require.NoError(t, err)
		spec := tailnetgateway.NodeSpec{
			IngressID: ingressID, OrganizationID: orgID, Slot: slot,
			Hostname: fmt.Sprintf("speakeasy-gw-%d", slot), ServiceName: serviceName, Tag: gatewayTag,
			AuthKey: "", Store: store, StateDir: t.TempDir(), ControlURL: controlURL, StartTimeout: time.Minute,
		}
		starts.Go(func() {
			node, err := runtime.Start(ctx, spec)
			if err != nil {
				t.Errorf("start slot %d: %v", slot, err)
				return
			}
			t.Cleanup(func() { _ = node.Close(context.Background()) })
			if err := node.Serve(peerEcho()); err != nil {
				t.Errorf("serve slot %d: %v", slot, err)
			}
			nodes[slot] = node
			go heartbeat(ctx, node)
		})
	}

	// The slot holding the lease has asked for the certificate, and Pebble
	// is holding that order open.
	require.Eventually(t, func() bool { return acme.orders.Load() >= 1 }, time.Minute, 100*time.Millisecond,
		"a slot asks for the certificate once approved")

	// Now a client reaches each slot before any certificate is stored.
	handshakes := sync.WaitGroup{}
	for hostname, addr := range addrs {
		handshakes.Go(func() {
			err := handshake(ctx, client, addr)
			t.Logf("handshake with %s before the certificate is stored: %v", hostname, err)
		})
	}
	handshakes.Wait()
	ordersWhileHeld := acme.orders.Load()

	acme.open()
	starts.Wait()
	require.Eventually(t, func() bool {
		for _, node := range nodes {
			if node == nil {
				return false
			}
			state, err := node.Status(ctx)
			if err != nil || !state.Online {
				return false
			}
		}
		return true
	}, 2*time.Minute, 250*time.Millisecond, "every slot comes online with the shared certificate")

	clientStatus, err := client.Up(ctx)
	require.NoError(t, err)
	clientAddr := clientStatus.TailscaleIPs[0]
	for hostname, addr := range addrs {
		require.NoError(t, handshake(ctx, client, addr), "a client completes the handshake with %s once the certificate is stored", hostname)
		proto, peer, err := request(ctx, client, addr)
		require.NoError(t, err, "a request to %s is served", hostname)
		require.Equal(t, "HTTP/2.0", proto, "%s offers HTTP/2", hostname)
		require.Equal(t, clientAddr.String(), peer, "%s learns the client's tailnet address from tsnet's PROXY header", hostname)
	}
	assert.Equal(t, int32(1), ordersWhileHeld, "no slot asks for a second certificate while the first is being issued")
	assert.Equal(t, int32(1), acme.finalized.Load(), "the connection gets exactly one certificate")
}

// TestRenewalNamesTheCertificateItReplaces holds the tsnet behaviour gateway
// renewal relies on: asking for a certificate valid for longer than the
// stored one has left renews it at once, returns the new certificate, stores
// it for the connection, and names the old one as replaced, which is what
// makes Let's Encrypt count it as a renewal exempt from its rate limits.
func TestRenewalNamesTheCertificateItReplaces(t *testing.T) { //nolint:paralleltest // sets process-wide state
	ctx := t.Context()
	acme := startACME(t)
	acme.open()
	control := startControl(t)

	db, err := infra.CloneTestDatabase(t, "tailnetrenewal")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db)
	encryption := orgencryptiontest.New(t, db)
	require.NoError(t, encryption.Provisioner.EnsureUseCase(ctx, orgID, tailnetgateway.EncryptionUseCase, orgencryption.SystemActor()))
	store, err := tailnetgateway.NewPostgresStateStore(db, encryption.Service, ingressID, orgID, 0)
	require.NoError(t, err)

	node := &tsnet.Server{Dir: t.TempDir(), Hostname: "renewer", ControlURL: control.HTTPTestServer.URL, Store: store, Ephemeral: true, Logf: logger.Discard}
	t.Cleanup(func() { _ = node.Close() })
	_, err = node.Up(ctx)
	require.NoError(t, err)
	lc, err := node.LocalClient()
	require.NoError(t, err)

	firstPEM, _, err := lc.CertPair(ctx, serviceFQDN)
	require.NoError(t, err)
	first := leafOf(t, firstPEM)
	require.Equal(t, []string{""}, acme.orderReplacements(), "first issuance replaces nothing")

	againPEM, _, err := lc.CertPair(ctx, serviceFQDN)
	require.NoError(t, err)
	require.Equal(t, first.SerialNumber, leafOf(t, againPEM).SerialNumber, "a stored certificate is returned as is")

	renewedPEM, _, err := lc.CertPairWithValidity(ctx, serviceFQDN, time.Until(first.NotAfter)+time.Hour)
	require.NoError(t, err)
	renewed := leafOf(t, renewedPEM)
	require.NotEqual(t, first.SerialNumber, renewed.SerialNumber, "the renewal returns the new certificate")
	require.Equal(t, []string{"", ariID(first)}, acme.orderReplacements(), "the renewal names the certificate it replaces")

	storedPEM, _, err := store.ReadTLSCertAndKey(serviceFQDN)
	require.NoError(t, err)
	require.Equal(t, renewed.SerialNumber, leafOf(t, storedPEM).SerialNumber, "the renewal is stored for every slot")
}

func leafOf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf
}

// ariID names a certificate as an ACME order's replaces field does.
func ariID(leaf *x509.Certificate) string {
	return base64.RawURLEncoding.EncodeToString(leaf.AuthorityKeyId) + "." + base64.RawURLEncoding.EncodeToString(leaf.SerialNumber.Bytes())
}
