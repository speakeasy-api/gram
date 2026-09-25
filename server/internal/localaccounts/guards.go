// Package localaccounts provides fail-closed tools for the selected local account.
package localaccounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// Config contains this worktree's effective configuration. No credentials are
// discovered here. ExternalProvidersConfigured covers all external billing
// providers, including Stripe and Polar.
type Config struct {
	// Root is the canonical secondary worktree directory.
	Root string

	// Environment must be local.
	Environment string

	// IDPBackend must select the local identity store.
	IDPBackend string

	// BillingProvider must be local.
	BillingProvider string

	// ExternalProvidersConfigured reports any external billing configuration.
	ExternalProvidersConfigured bool

	// PGService must be empty to disable libpq service discovery.
	PGService string

	// DockerHost identifies the local Docker endpoint.
	DockerHost string

	// DockerContext identifies the local Docker context.
	DockerContext string

	// DatabaseURL is the explicit local Gram PostgreSQL URL.
	DatabaseURL string

	// IDPDatabase is the file URI of this worktree's SQLite identity store.
	IDPDatabase string

	// IDPURL is the loopback IdP base URL ending in /oauth2-1.
	IDPURL string

	// ExpectedDatabasePort is the worktree PostgreSQL host port.
	ExpectedDatabasePort uint16

	// ExpectedIDPPort is the worktree IdP host port.
	ExpectedIDPPort uint16

	// RedisAddress is the loopback Redis address.
	RedisAddress string

	// ExpectedRedisPort is the worktree Redis host port.
	ExpectedRedisPort uint16

	// ComposeProject identifies this worktree's Compose containers.
	ComposeProject string

	// TemporalAddress is the loopback Temporal address.
	TemporalAddress string

	// TemporalNamespace must equal ComposeProject and cannot be default.
	TemporalNamespace string

	// TemporalTaskQueue is the worktree Temporal task queue.
	TemporalTaskQueue string
}

// Target identifies the sole account linked in both local stores.
type Target struct {
	// UserID is the Gram user primary key.
	UserID string

	// OrganizationID is the Gram organization primary key.
	OrganizationID string

	// WorkOSUserID is the local IdP UUID mapped to a WorkOS-style subject.
	WorkOSUserID string

	// WorkOSOrganizationID is the shared external organization link.
	WorkOSOrganizationID string
}

// Validate checks configuration and the database/Redis containers' actual
// Compose worktree labels and port bindings. It does not connect to PostgreSQL.
func Validate(ctx context.Context, c Config) (*pgxpool.Config, error) {
	pc, err := validateConfig(c)
	if err != nil {
		return nil, err
	}
	if err := checkLocalDocker(ctx, c); err != nil {
		return nil, err
	}
	if err := checkContainer(ctx, c, "gram-db", "5432/tcp", c.ExpectedDatabasePort); err != nil {
		return nil, err
	}
	if err := checkContainer(ctx, c, "gram-cache", "35299/tcp", c.ExpectedRedisPort); err != nil {
		return nil, err
	}
	return pc, nil
}

func validateConfig(c Config) (*pgxpool.Config, error) {
	gitInfo, gitErr := os.Stat(filepath.Join(c.Root, ".git"))
	if (gitErr == nil && gitInfo.IsDir()) || c.ComposeProject == "" || c.TemporalNamespace == "default" {
		return nil, errors.New("local account profiles support initialized secondary worktrees only; create a secondary Git worktree, run ./zero there, and retry with an explicit Compose project and matching non-default Temporal namespace")
	}
	if c.Environment != "local" {
		return nil, errors.New("local accounts require GRAM_ENVIRONMENT=local")
	}
	if c.IDPBackend != "local" {
		return nil, errors.New("local accounts require explicit GRAM_DEVIDP_BACKEND=local")
	}
	if c.BillingProvider != "local" || c.ExternalProvidersConfigured {
		return nil, errors.New("local accounts require the local billing stub: unset external billing API keys and remote Temporal credentials before running")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, errors.New("cannot determine worktree root")
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil || !filepath.IsAbs(c.Root) || filepath.Clean(c.Root) != c.Root || cwd != c.Root {
		return nil, errors.New("root must be the canonical current worktree directory")
	}
	root, err := filepath.EvalSymlinks(c.Root)
	if err != nil || root != c.Root {
		return nil, errors.New("root must not contain symlinks")
	}
	for _, name := range []string{".git", "compose.yml", "go.mod"} {
		if _, err := os.Stat(filepath.Join(c.Root, name)); err != nil {
			return nil, errors.New("root is not a Gram repository worktree")
		}
	}
	expected := filepath.Join(c.Root, "local", "devidp", "devidp.db")
	if c.IDPDatabase != "file:"+expected {
		return nil, errors.New("IdP database must be this worktree's local/devidp/devidp.db")
	}
	actual, err := filepath.EvalSymlinks(expected)
	if err != nil || actual != expected {
		return nil, errors.New("IdP database must exist without symlink redirection")
	}
	if c.ExpectedDatabasePort == 0 || c.ExpectedIDPPort == 0 || !safeName.MatchString(c.ComposeProject) {
		return nil, errors.New("explicit worktree ports and Compose project are required")
	}
	if _, err := idpURL(c); err != nil {
		return nil, err
	}
	if err := validateRedis(c); err != nil {
		return nil, err
	}
	return databaseConfig(c)
}

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func localHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func idpURL(c Config) (*url.URL, error) {
	u, err := url.Parse(c.IDPURL)
	if err != nil || u.Scheme != "http" || !localHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/oauth2-1" || u.Port() != strconv.Itoa(int(c.ExpectedIDPPort)) {
		return nil, errors.New("IdP URL must be the expected loopback HTTP oauth2-1 endpoint")
	}
	return u, nil
}

func databaseConfig(c Config) (*pgxpool.Config, error) {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.User.Username() != "gram" || u.Path != "/gram" || u.Fragment != "" {
		return nil, errors.New("database must be an explicit PostgreSQL URL for gram user and gram database")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("invalid database URL parameters")
	}
	for k, values := range q {
		if len(values) != 1 {
			return nil, errors.New("duplicate database URL parameter")
		}
		if k != "sslmode" && k != "search_path" && k != "connect_timeout" {
			return nil, errors.New("unsupported database URL parameter")
		}
	}
	if values, present := q["search_path"]; present && values[0] != "public" {
		return nil, errors.New("local database search_path must be public")
	}
	q.Set("search_path", "public")
	if q.Get("sslmode") != "disable" {
		return nil, errors.New("local database requires sslmode=disable")
	}
	if !localHost(u.Hostname()) || u.Port() != strconv.Itoa(int(c.ExpectedDatabasePort)) {
		return nil, errors.New("database must use the expected loopback port")
	}
	// Override libpq discovery: no service, TLS key or credential files are read.
	if c.PGService != "" {
		return nil, errors.New("PGSERVICE must be unset for local account tooling")
	}
	q.Set("options", "")
	q.Set("passfile", os.DevNull)
	q.Set("sslcert", "")
	q.Set("sslkey", "")
	q.Set("sslrootcert", "")
	q.Set("sslpassword", "")
	password, _ := u.User.Password()
	q.Set("password", password)
	u.RawQuery = q.Encode()
	pc, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, errors.New("invalid local database configuration")
	}
	cc := pc.ConnConfig
	if cc.User != "gram" || cc.Database != "gram" || !localHost(cc.Host) || cc.Port != c.ExpectedDatabasePort || cc.TLSConfig != nil {
		return nil, errors.New("unsafe primary database endpoint")
	}
	for _, f := range cc.Fallbacks {
		if !localHost(f.Host) || f.Port != c.ExpectedDatabasePort || f.TLSConfig != nil {
			return nil, errors.New("unsafe fallback database endpoint")
		}
	}
	cc.LookupFunc = func(_ context.Context, host string) ([]string, error) {
		if !localHost(host) {
			return nil, errors.New("non-loopback database lookup refused")
		}
		if host == "localhost" {
			return []string{"127.0.0.1", "::1"}, nil
		}
		return []string{host}, nil
	}
	cc.ConnectTimeout = 5 * time.Second
	return pc, nil
}

func commandOutput(ctx context.Context, root, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch name {
	case "docker":
		cmd = exec.CommandContext(ctx, "docker", args...) //nolint:gosec // Fixed executable, no shell; internal callers supply read-only commands and validated local identifiers.
	case "pitchfork":
		cmd = exec.CommandContext(ctx, "pitchfork", args...) //nolint:gosec // Fixed executable, no shell; internal callers only inspect worktree daemon status.
	default:
		return nil, errors.New("unsupported read-only safety command")
	}
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s read-only safety check failed", name)
	}
	return out, nil
}

type containerEvidence struct {
	Labels map[string]string `json:"Labels"`
	Ports  map[string][]struct {
		HostIP   string `json:"HostIP"`
		HostPort string `json:"HostPort"`
	} `json:"Ports"`
	Running bool `json:"Running"`
}

func validateRedis(c Config) error {
	host, port, err := net.SplitHostPort(c.RedisAddress)
	if err != nil || !localHost(host) || c.ExpectedRedisPort == 0 || port != strconv.Itoa(int(c.ExpectedRedisPort)) {
		return errors.New("redis must use this worktree's expected loopback address and port")
	}
	return nil
}

func checkLocalDocker(ctx context.Context, c Config) error {
	endpoint := c.DockerHost
	if endpoint == "" || c.DockerContext != "" {
		out, err := commandOutput(ctx, c.Root, "docker", "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}")
		if err != nil {
			return err
		}
		if json.Unmarshal(out, &endpoint) != nil {
			return errors.New("cannot verify local Docker endpoint")
		}
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "unix" || u.Host != "" || !filepath.IsAbs(u.Path) {
		return errors.New("docker must use a local Unix socket")
	}
	return nil
}

func checkContainer(ctx context.Context, c Config, service, targetPort string, hostPort uint16) error {
	out, err := commandOutput(ctx, c.Root, "docker", "ps", "--filter", "label=com.docker.compose.project="+c.ComposeProject, "--filter", "label=com.docker.compose.service="+service, "--format", "{{.ID}}")
	if err != nil {
		return err
	}
	ids := strings.Fields(string(out))
	if len(ids) != 1 {
		return fmt.Errorf("expected exactly one running worktree %s container", service)
	}
	out, err = commandOutput(ctx, c.Root, "docker", "inspect", "--format", `{"Labels":{{json .Config.Labels}},"Ports":{{json .NetworkSettings.Ports}},"Running":{{json .State.Running}}}`, ids[0])
	if err != nil {
		return err
	}
	var e containerEvidence
	if json.Unmarshal(out, &e) != nil {
		return errors.New("invalid local container evidence")
	}
	return validateServiceContainer(c, e, service, targetPort, hostPort)
}

func validateServiceContainer(c Config, e containerEvidence, service, targetPort string, hostPort uint16) error {
	if !e.Running || e.Labels["com.docker.compose.project"] != c.ComposeProject || e.Labels["com.docker.compose.service"] != service || e.Labels["com.docker.compose.project.working_dir"] != c.Root {
		return errors.New("container does not belong to this worktree")
	}
	ports := e.Ports[targetPort]
	if len(ports) == 0 {
		return errors.New("container has no expected published port")
	}
	for _, p := range ports {
		if p.HostPort != strconv.Itoa(int(hostPort)) || (p.HostIP != "0.0.0.0" && p.HostIP != "::" && !localHost(p.HostIP)) {
			return errors.New("container port does not match local configuration")
		}
	}
	return nil
}

type Queryer = repo.DBTX

// Resolve only reads the selected oauth2-1 identity. It never bootstraps or
// switches a user. Validate before constructing the pool; recheck the returned
// target inside the runner transaction before any mutation.
func Resolve(ctx context.Context, pool Queryer, c Config) (Target, error) {
	if _, err := validateConfig(c); err != nil {
		return Target{}, err
	}
	if pool == nil {
		return Target{}, errors.New("local database pool is required")
	}
	id, err := selectedUser(ctx, c)
	if err != nil {
		return Target{}, err
	}
	workosID := devidentity.WorkOSUserID(id)
	rows, err := repo.New(pool).ResolveAccountTargets(ctx, conv.ToPGText(workosID))
	if err != nil {
		return Target{}, errors.New("cannot resolve selected local account")
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, Target{UserID: row.ID, OrganizationID: row.OrganizationID, WorkOSUserID: row.WorkosID.String, WorkOSOrganizationID: row.WorkosOrganizationID})
	}
	organizationID, err := selectedOrganization(ctx, c, id)
	if err != nil {
		return Target{}, err
	}
	return soleTarget(targets, workosID, organizationID)
}

func soleTarget(targets []Target, workosID, organizationID string) (Target, error) {
	if len(targets) != 1 {
		return Target{}, errors.New("selected identity must have exactly one active Gram membership; log in locally first or resolve ambiguity")
	}
	t := targets[0]
	if t.UserID == "" || t.OrganizationID == "" || t.WorkOSUserID != workosID || organizationID == "" || t.WorkOSOrganizationID != organizationID {
		return Target{}, errors.New("selected identity requires an active organization with an external organization link")
	}
	return t, nil
}

func selectedUser(ctx context.Context, c Config) (uuid.UUID, error) {
	body, err := readLocalIDP(ctx, c, "/rpc/devIdp.getCurrentUser", `{"mode":"oauth2-1"}`)
	if err != nil {
		return uuid.Nil, err
	}
	return decodeSelected(bytes.NewReader(body), c)
}

func readLocalIDP(ctx context.Context, c Config, path, payload string) ([]byte, error) {
	u, err := idpURL(c)
	if err != nil {
		return nil, err
	}
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewBufferString(payload))
	if err != nil {
		return nil, errors.New("cannot construct selected identity request")
	}
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !localHost(host) {
			return nil, errors.New("non-loopback IdP dial refused")
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(host, port))
	}}
	defer transport.CloseIdleConnections()
	// The standard guardian client inherits proxy discovery. This isolated transport
	// must bypass proxies and DNS, and permit only literal loopback destinations.
	hc := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("IdP redirects refused") }} //nolint:forbidigo // Local-only safety probe requires the proxy-free, loopback-only transport above.
	resp, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("cannot read selected local identity; start only the local IdP if needed")
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("no readable selected oauth2-1 identity; select one in the local IdP first")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read local IdP response: %w", err)
	}
	return body, nil
}

// Local IdP memberships are hard-deleted, not status-filtered. Their organization
// UUID is not Gram's organization ID or its WorkOS link; read the stored link
// explicitly instead of using the WorkOS emulation API's UUID fallback.
func selectedOrganization(ctx context.Context, c Config, userID uuid.UUID) (string, error) {
	body, err := readLocalIDP(ctx, c, "/rpc/memberships.list", fmt.Sprintf(`{"user_id":%q,"limit":2}`, userID.String()))
	if err != nil {
		return "", err
	}
	var memberships struct {
		Items []struct {
			UserID         string `json:"user_id"`
			OrganizationID string `json:"organization_id"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if json.Unmarshal(body, &memberships) != nil || len(memberships.Items) != 1 || memberships.NextCursor == nil || *memberships.NextCursor != "" {
		return "", errors.New("selected local identity must have exactly one active IdP membership")
	}
	membership := memberships.Items[0]
	orgID, err := uuid.Parse(membership.OrganizationID)
	if err != nil || orgID == uuid.Nil || orgID.String() != membership.OrganizationID || membership.UserID != userID.String() {
		return "", errors.New("selected local identity has an invalid IdP membership link")
	}
	cursor := ""
	seen := make(map[string]bool)
	for {
		payload, err := json.Marshal(map[string]any{"cursor": cursor, "limit": 100})
		if err != nil {
			return "", fmt.Errorf("encode local organization request: %w", err)
		}
		body, err := readLocalIDP(ctx, c, "/rpc/organizations.list", string(payload))
		if err != nil {
			return "", err
		}
		var organizations struct {
			Items []struct {
				ID       string `json:"id"`
				WorkOSID string `json:"workos_id"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		if json.Unmarshal(body, &organizations) != nil || organizations.NextCursor == nil {
			return "", errors.New("invalid local IdP organization response")
		}
		for _, org := range organizations.Items {
			if org.ID == membership.OrganizationID {
				if strings.TrimSpace(org.WorkOSID) == "" {
					return "", errors.New("selected local IdP organization has no external organization link")
				}
				return org.WorkOSID, nil
			}
		}
		cursor = *organizations.NextCursor
		if cursor == "" || seen[cursor] {
			return "", errors.New("selected local IdP organization not found")
		}
		seen[cursor] = true
	}
}

func decodeSelected(r io.Reader, c Config) (uuid.UUID, error) {
	var v struct {
		Mode string `json:"mode"`
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
		Provenance *struct {
			Backend      string `json:"backend"`
			WorktreeRoot string `json:"worktree_root"`
			DatabasePath string `json:"database_path"`
		} `json:"provenance"`
	}
	if json.NewDecoder(r).Decode(&v) != nil || v.Mode != "oauth2-1" || v.User == nil {
		return uuid.Nil, errors.New("invalid selected oauth2-1 identity response")
	}
	// Compare the responding daemon's evidence, not client-supplied configuration.
	// Validate has already established canonical, non-symlinked expected paths.
	p := v.Provenance
	if p == nil || p.Backend != "local" || !filepath.IsAbs(c.Root) || p.WorktreeRoot != c.Root || p.DatabasePath != filepath.Join(c.Root, "local", "devidp", "devidp.db") || "file:"+p.DatabasePath != c.IDPDatabase {
		return uuid.Nil, errors.New("selected IdP identity lacks matching local worktree and database provenance; use this worktree's updated local IdP")
	}
	id, err := uuid.Parse(v.User.ID)
	if err != nil || id == uuid.Nil || id.String() != v.User.ID {
		return uuid.Nil, errors.New("selected local identity must contain a canonical nonzero UUID")
	}
	return id, nil
}

// CheckQuiescent is a read-only prerequisite, not a lock. Keep server and worker
// stopped until the command exits; recheck immediately before the transaction.
// Unknown daemon state and unavailable Temporal fail closed.
func CheckQuiescent(ctx context.Context, c Config) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := validateConfig(c); err != nil {
		return err
	}
	if err := validateTemporal(c); err != nil {
		return err
	}
	for _, name := range []string{"server", "worker"} {
		out, err := commandOutput(ctx, c.Root, "pitchfork", "status", filepath.Base(c.Root)+"/"+name, "--json")
		if err != nil {
			return err
		}
		if err := stoppedWorktreeDaemon(out, filepath.Base(c.Root), name); err != nil {
			return err
		}
	}
	return CheckWorkflows(ctx, c)
}

// CheckWorkflows refuses queued or retrying trial demotions, even for a plan.
func CheckWorkflows(ctx context.Context, c Config) error {
	if err := validateTemporal(c); err != nil {
		return err
	}
	address := c.TemporalAddress
	host, port, _ := net.SplitHostPort(address)
	if host == "localhost" {
		address = net.JoinHostPort("127.0.0.1", port)
	}
	tc, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: c.TemporalNamespace, ConnectionOptions: client.ConnectionOptions{DialOptions: []grpc.DialOption{grpc.WithNoProxy()}}})
	if err != nil {
		return errors.New("cannot verify local Temporal quiescence")
	}
	defer tc.Close()
	result, err := tc.WorkflowService().CountWorkflowExecutions(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Namespace: c.TemporalNamespace,
		Query:     "ExecutionStatus = 'Running' AND WorkflowType = 'DemoteExpiredTrialsWorkflow'",
	})
	if err != nil || result == nil || result.Count != 0 {
		return errors.New("local Temporal must have no open trial demotion workflows")
	}
	return nil
}

func validateTemporal(c Config) error {
	host, port, err := net.SplitHostPort(c.TemporalAddress)
	n, pe := strconv.Atoi(port)
	if err != nil || pe != nil || n < 1 || n > 65535 || !localHost(host) || c.TemporalNamespace != c.ComposeProject || c.TemporalNamespace == "default" || !safeName.MatchString(c.TemporalNamespace) || !safeName.MatchString(c.TemporalTaskQueue) {
		return errors.New("temporal must use a loopback endpoint and this worktree's explicit namespace and queue")
	}
	return nil
}

func stoppedDaemon(data []byte, name string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields["pid"] == nil {
		return errors.New("daemon status has no explicit process evidence")
	}
	var s struct {
		Name   string `json:"name"`
		PID    *int   `json:"pid"`
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &s) != nil || s.Name != name || s.PID != nil || (s.Status != "stopped" && s.Status != "available") {
		return fmt.Errorf("%s must be stopped in pitchfork before changing local accounts", name)
	}
	return nil
}

// A stopped daemon in another worktree is not evidence about this worker.
func stoppedWorktreeDaemon(data []byte, namespace, name string) error {
	var v struct {
		ID        string `json:"id"`
		Namespace string `json:"namespace"`
	}
	if json.Unmarshal(data, &v) != nil || v.Namespace != namespace || v.ID != namespace+"/"+name {
		return errors.New("daemon evidence does not belong to this worktree")
	}
	return stoppedDaemon(data, name)
}
