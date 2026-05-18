package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type cloudSQLInstance struct {
	projectID string
	region    string
	instance  string
}

type cloudSQLAccessMode string

const (
	cloudSQLAccessModeRead  cloudSQLAccessMode = "read-only"
	cloudSQLAccessModeWrite cloudSQLAccessMode = "write"
)

func startCloudSQLProxy(ctx context.Context, opts options, readOnly bool) (string, func(), error) {
	instance, err := cloudSQLInstanceForEnvironment(opts.environment)
	if err != nil {
		return "", nil, err
	}
	port := opts.cloudSQLPort
	if port == 0 {
		port, err = freeLocalPort()
		if err != nil {
			return "", nil, err
		}
	}
	user, err := activeGCloudAccount(ctx)
	if err != nil {
		return "", nil, err
	}

	instancePath := fmt.Sprintf("%s:%s:%s", instance.projectID, instance.region, instance.instance)
	fmt.Printf("Starting Cloud SQL proxy for %s on 127.0.0.1:%d as %s\n", instancePath, port, user)

	// #nosec G204 -- instancePath is selected from hardcoded dev/prod Cloud SQL config and port is validated before use.
	cmd := exec.CommandContext(ctx, "cloud-sql-proxy", fmt.Sprintf("%s?port=%d", instancePath, port), "--auto-iam-authn")
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("start cloud-sql-proxy: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if cmd.ProcessState != nil {
				return
			}
			select {
			case <-errCh:
				return
			default:
			}
			if cmd.Process == nil {
				return
			}
			_ = cmd.Process.Kill()
			select {
			case <-errCh:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-errCh
			}
		})
	}

	if err := waitForTCP(ctx, "127.0.0.1", port, errCh); err != nil {
		cleanup()
		return "", nil, err
	}

	mode := cloudSQLModeForReadOnly(readOnly)
	if err := prepareCloudSQLIAMAccess(ctx, instance, port, strings.TrimSpace(opts.cloudSQLDBName), user, mode); err != nil {
		cleanup()
		return "", nil, err
	}

	databaseURL := cloudSQLDatabaseURL("127.0.0.1", port, strings.TrimSpace(opts.cloudSQLDBName), user)
	return databaseURL, cleanup, nil
}

func cloudSQLInstanceForEnvironment(env environment) (cloudSQLInstance, error) {
	switch env {
	case envDev:
		return cloudSQLInstance{
			projectID: "linen-analyst-344721",
			region:    "us-west1",
			instance:  "gram-dev-instance",
		}, nil
	case envProd:
		return cloudSQLInstance{
			projectID: "speakeasy-prod-354914",
			region:    "us-west1",
			instance:  "gram-prod-instance",
		}, nil
	default:
		return cloudSQLInstance{
			projectID: "",
			region:    "",
			instance:  "",
		}, fmt.Errorf("cloud sql proxy is not configured for environment %q", env)
	}
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free local port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("free local port listener did not return a TCP address")
	}
	return addr.Port, nil
}

func activeGCloudAccount(ctx context.Context) (string, error) {
	output, err := gcloudOutput(ctx, "auth", "list", "--format=value(ACCOUNT)")
	if err != nil {
		return "", err
	}
	var fallback string
	for line := range strings.SplitSeq(output, "\n") {
		account := strings.TrimSpace(line)
		if account == "" {
			continue
		}
		if fallback == "" {
			fallback = account
		}
		if strings.Contains(strings.ToLower(account), "speakeasy") {
			return account, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", errors.New("no active gcloud account found")
}

func gcloudOutput(ctx context.Context, args ...string) (string, error) {
	// #nosec G204 -- callers pass fixed gcloud subcommands with validated env-derived values.
	cmd := exec.CommandContext(ctx, "gcloud", args...)
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", fmt.Errorf("gcloud %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return "", fmt.Errorf("gcloud %s: %w", strings.Join(args, " "), err)
}

func prepareCloudSQLIAMAccess(ctx context.Context, instance cloudSQLInstance, port int, dbName, user string, mode cloudSQLAccessMode) error {
	fmt.Printf("Preparing Cloud SQL IAM database access mode=%s user=%s\n", mode, user)
	if err := ensureCloudSQLIAMUser(ctx, instance, user); err != nil {
		return err
	}

	password, err := cloudSQLAdminPassword(ctx, instance)
	if err != nil {
		return err
	}
	adminURL := cloudSQLAdminDatabaseURL("127.0.0.1", port, dbName, password)
	adminDB, err := connectDB(ctx, adminURL, false, defaultStatementTimeout)
	if err != nil {
		return fmt.Errorf("connect Cloud SQL admin database: %w", err)
	}
	defer adminDB.Close()

	if err := grantCloudSQLIAMUserAccess(ctx, adminDB, user, mode); err != nil {
		return err
	}
	return nil
}

func ensureCloudSQLIAMUser(ctx context.Context, instance cloudSQLInstance, user string) error {
	fmt.Println("Looking up Cloud SQL IAM database user")
	output, err := gcloudOutput(ctx,
		"sql",
		"users",
		"list",
		"--instance", instance.instance,
		"--format=value(NAME)",
		"--project", instance.projectID,
	)
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(output, "\n") {
		if strings.TrimSpace(line) == user {
			return nil
		}
	}

	fmt.Printf("Creating Cloud SQL IAM database user %s on %s\n", user, instance.instance)
	_, err = gcloudOutput(ctx,
		"sql",
		"users",
		"create", user,
		"--instance", instance.instance,
		"--type=cloud_iam_user",
		"--project", instance.projectID,
	)
	if err != nil {
		return fmt.Errorf("create Cloud SQL IAM database user %s: %w", user, err)
	}
	return nil
}

func cloudSQLAdminPassword(ctx context.Context, instance cloudSQLInstance) (string, error) {
	envName, err := cloudSQLSecretEnvironment(instance)
	if err != nil {
		return "", err
	}
	output, err := gcloudOutput(ctx,
		"secrets",
		"versions",
		"access", "latest",
		"--secret", fmt.Sprintf("%s_gram_db_password", envName),
		"--project", instance.projectID,
	)
	if err != nil {
		return "", fmt.Errorf("read Cloud SQL admin password secret: %w", err)
	}
	password := strings.TrimSpace(output)
	if password == "" {
		return "", errors.New("cloud SQL admin password secret was empty")
	}
	return password, nil
}

func cloudSQLSecretEnvironment(instance cloudSQLInstance) (string, error) {
	switch instance.instance {
	case "gram-dev-instance":
		return string(envDev), nil
	case "gram-prod-instance":
		return string(envProd), nil
	default:
		return "", fmt.Errorf("no Cloud SQL password secret configured for instance %q", instance.instance)
	}
}

type dbExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func grantCloudSQLIAMUserAccess(ctx context.Context, db dbExecer, user string, mode cloudSQLAccessMode) error {
	schema := pgx.Identifier{"gram"}.Sanitize()
	grantee := pgx.Identifier{user}.Sanitize()
	statements := []string{
		fmt.Sprintf("REVOKE ALL ON ALL TABLES IN SCHEMA %s FROM %s", schema, grantee),
		fmt.Sprintf("REVOKE USAGE, CREATE ON SCHEMA %s FROM %s", schema, grantee),
	}
	switch mode {
	case cloudSQLAccessModeRead:
		statements = append(statements,
			fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schema, grantee),
			fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", schema, grantee),
		)
	case cloudSQLAccessModeWrite:
		statements = append(statements,
			fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA %s TO %s", schema, grantee),
			fmt.Sprintf("GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA %s TO %s", schema, grantee),
		)
	default:
		return fmt.Errorf("unsupported Cloud SQL access mode %q", mode)
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("grant Cloud SQL IAM user access with %q: %w", statement, err)
		}
	}
	return nil
}

func waitForTCP(ctx context.Context, host string, port int, proxyErr <-chan error) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for cloud-sql-proxy: %w", ctx.Err())
		case err := <-proxyErr:
			return fmt.Errorf("cloud-sql-proxy exited before accepting connections: %w", err)
		case <-deadline:
			return fmt.Errorf("cloud-sql-proxy did not accept connections on %s within 10s", address)
		case <-ticker.C:
		}
	}
}

func cloudSQLDatabaseURL(host string, port int, dbName, user string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.User(user),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("search_path", "gram")
	u.RawQuery = q.Encode()
	return u.String()
}

func cloudSQLAdminDatabaseURL(host string, port int, dbName, password string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword("gram", password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("search_path", "gram")
	u.RawQuery = q.Encode()
	return u.String()
}

func cloudSQLProxyHint() string {
	return "; cloud sql proxy is enabled, so check that gcloud auth is active and the Cloud SQL proxy can reach the instance"
}

func cloudSQLRunHint(err error, opts options) string {
	if !opts.cloudSQLProxy {
		return ""
	}
	message := err.Error()
	if !strings.Contains(message, "SQLSTATE 42501") && !strings.Contains(message, "permission denied") {
		return ""
	}
	return fmt.Sprintf(`

Cloud SQL permission hint:
  The script attempted to grant %s access to your IAM database user before connecting.
  Check that the gram admin password secret is current and that your gcloud account can manage Cloud SQL users.`, cloudSQLModeForReadOnly(opts.dryRun || opts.phase == phasePreflight || opts.phase == phaseValidate))
}

func cloudSQLModeForReadOnly(readOnly bool) cloudSQLAccessMode {
	if readOnly {
		return cloudSQLAccessModeRead
	}
	return cloudSQLAccessModeWrite
}
