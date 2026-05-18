package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cloudSQLInstance struct {
	projectID string
	region    string
	instance  string
}

func startCloudSQLProxy(ctx context.Context, opts options) (string, func(), error) {
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
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
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
			if err := cmd.Process.Signal(os.Interrupt); err != nil {
				_ = cmd.Process.Kill()
			}
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
	// #nosec G204 -- this helper is only used with fixed gcloud auth list arguments.
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

func cloudSQLProxyHint() string {
	return "; cloud sql proxy is enabled, so check that your IAM database user exists and has the required READ or ALL grants from gram-infra"
}
