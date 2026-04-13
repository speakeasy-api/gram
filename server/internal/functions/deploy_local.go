package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	localRunnerBinaryName    = "gram-runner-local"
	localRunnerCodeZipName   = "code.zip"
	localRunnerMetadataName  = "deployment.json"
	localRunnerHealthTimeout = 10 * time.Second
)

type localRunnerDeployment struct {
	BearerSecret string  `json:"bearerSecret"`
	DeploymentID string  `json:"deploymentId"`
	FunctionID   string  `json:"functionId"`
	ProjectID    string  `json:"projectId"`
	Runtime      Runtime `json:"runtime"`
}

type LocalRunner struct {
	logger         *slog.Logger
	codeRootDir    string
	serverURL      *url.URL
	assetStore     assets.BlobStore
	httpClient     *guardian.HTTPClient
	proxyServerURL string

	proxyOnce  sync.Once
	proxyErr   error
	binaryOnce sync.Once
	binaryErr  error
	binaryPath string
}

var _ Orchestrator = (*LocalRunner)(nil)

func NewLocalRunner(logger *slog.Logger, tracerProvider trace.TracerProvider, codeRootDir string, serverURL *url.URL, assetStore assets.BlobStore) *LocalRunner {
	policy := must.Value(guardian.NewUnsafePolicy(tracerProvider, []string{}))
	httpClient := policy.PooledClient()
	httpClient.Timeout = 30 * time.Second

	return &LocalRunner{
		logger:         logger.With(attr.SlogComponent("local-functions-orchestrator")),
		codeRootDir:    filepath.Clean(codeRootDir),
		serverURL:      serverURL,
		assetStore:     assetStore,
		httpClient:     httpClient,
		proxyServerURL: "",
		proxyOnce:      sync.Once{},
		proxyErr:       nil,
		binaryOnce:     sync.Once{},
		binaryErr:      nil,
		binaryPath:     "",
	}
}

func (l *LocalRunner) ToolCall(ctx context.Context, req RunnerToolCallRequest) (*http.Request, error) {
	logger := l.logger.With(
		attr.SlogFunctionsBackend("local"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionsID.String()),
		attr.SlogToolURN(req.ToolURN.String()),
		attr.SlogToolName(req.ToolName),
	)

	if err := invCheckLocalToolCall(req); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "malformed local tool call request").Log(ctx, logger)
	}

	deployment, err := l.loadDeployment(req.FunctionsID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "local function runner not found").Log(ctx, logger)
	}

	enc, err := encryption.New(deployment.BearerSecret)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create local function auth client").Log(ctx, logger)
	}

	token, err := TokenV1(enc, TokenRequestV1{
		ID:      req.InvocationID.String(),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
		Subject: req.ToolURN.String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create bearer token for local function tool call").Log(ctx, logger)
	}

	payload, err := json.Marshal(CallToolPayload{
		ToolName:    req.ToolName,
		Input:       req.Input,
		Environment: req.Environment,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal local function tool payload").Log(ctx, logger)
	}

	endpoint, err := l.proxyEndpoint(req.FunctionsID, "tool-call")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve local proxy endpoint").Log(ctx, logger)
	}

	httpreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create local function tool request").Log(ctx, logger)
	}

	httpreq.Header.Set("Authorization", "Bearer "+token)
	httpreq.Header.Set("Content-Type", "application/json")

	return httpreq, nil
}

func (l *LocalRunner) ReadResource(ctx context.Context, req RunnerResourceReadRequest) (*http.Request, error) {
	logger := l.logger.With(
		attr.SlogFunctionsBackend("local"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionsID.String()),
		attr.SlogResourceURI(req.ResourceURI),
		attr.SlogResourceURN(req.ResourceURN.String()),
	)

	if err := invCheckLocalReadResource(req); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "malformed local read resource request").Log(ctx, logger)
	}

	deployment, err := l.loadDeployment(req.FunctionsID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "local function runner not found").Log(ctx, logger)
	}

	enc, err := encryption.New(deployment.BearerSecret)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create local function auth client").Log(ctx, logger)
	}

	token, err := TokenV1(enc, TokenRequestV1{
		ID:      req.InvocationID.String(),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
		Subject: req.ResourceURN.String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create bearer token for local function read resource call").Log(ctx, logger)
	}

	payload, err := json.Marshal(ReadResourcePayload{
		URI:         req.ResourceURI,
		Input:       req.Input,
		Environment: req.Environment,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal local function read resource payload").Log(ctx, logger)
	}

	endpoint, err := l.proxyEndpoint(req.FunctionsID, "resource-request")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve local proxy endpoint").Log(ctx, logger)
	}

	httpreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create local function resource request").Log(ctx, logger)
	}

	httpreq.Header.Set("Authorization", "Bearer "+token)
	httpreq.Header.Set("Content-Type", "application/json")

	return httpreq, nil
}

func (l *LocalRunner) Deploy(ctx context.Context, req RunnerDeployRequest) (*RunnerDeployResult, error) {
	logger := l.logger.With(
		attr.SlogFunctionsBackend("local"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionID.String()),
		attr.SlogDeploymentFunctionsAccessID(req.AccessID.String()),
		attr.SlogFunctionsRuntime(req.Runtime),
	)

	if err := invCheckLocalDeploy(req); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "malformed local function deployment").Log(ctx, logger)
	}

	functionDir := l.functionDir(req.FunctionID)
	if err := os.RemoveAll(functionDir); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear local function directory").Log(ctx, logger)
	}
	if err := os.MkdirAll(functionDir, 0750); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create local function directory").Log(ctx, logger)
	}

	codePath := filepath.Join(functionDir, localRunnerCodeZipName)
	if err := l.copyAsset(ctx, req.Assets[0].AssetURL, codePath); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "copy local function asset").Log(ctx, logger)
	}

	deployment := localRunnerDeployment{
		BearerSecret: req.BearerSecret,
		DeploymentID: req.DeploymentID.String(),
		FunctionID:   req.FunctionID.String(),
		ProjectID:    req.ProjectID.String(),
		Runtime:      req.Runtime,
	}
	if err := writeJSONAtomic(filepath.Join(functionDir, localRunnerMetadataName), deployment, 0600); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "write local function deployment metadata").Log(ctx, logger)
	}

	name := fmt.Sprintf("dev-%s", req.FunctionID.String())
	return &RunnerDeployResult{
		URN:       urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "local", name),
		PublicURL: nil,
		Version:   "dev",
		Provider:  "local",
		Region:    "local",
		Scale:     1,
	}, nil
}

func (l *LocalRunner) Reap(ctx context.Context, req ReapRequest) error {
	logger := l.logger.With(
		attr.SlogFunctionsBackend("local"),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(req.FunctionID.String()),
	)

	if req.FunctionID == uuid.Nil {
		return oops.E(oops.CodeInvariantViolation, nil, "local function reap request is missing function id").Log(ctx, logger)
	}

	if err := os.RemoveAll(l.functionDir(req.FunctionID)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove local function deployment").Log(ctx, logger)
	}

	return nil
}

func (l *LocalRunner) proxyEndpoint(functionID uuid.UUID, action string) (string, error) {
	if err := l.ensureProxyServer(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/functions/%s/%s", l.proxyServerURL, functionID.String(), action), nil
}

func (l *LocalRunner) ensureProxyServer() error {
	l.proxyOnce.Do(func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			l.proxyErr = fmt.Errorf("listen for local functions proxy: %w", err)
			return
		}

		mux := http.NewServeMux()
		mux.HandleFunc("POST /functions/{functionID}/tool-call", l.handleLocalProxyRequest("tool-call"))
		mux.HandleFunc("POST /functions/{functionID}/resource-request", l.handleLocalProxyRequest("resource-request"))

		server := &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 30 * time.Second,
		}
		l.proxyServerURL = "http://" + listener.Addr().String()

		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				l.logger.ErrorContext(context.Background(), "local functions proxy exited", attr.SlogError(err))
			}
		}()
	})

	return l.proxyErr
}

func (l *LocalRunner) handleLocalProxyRequest(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		functionID, err := uuid.Parse(r.PathValue("functionID"))
		if err != nil {
			http.Error(w, "invalid function id", http.StatusBadRequest)
			return
		}

		deployment, err := l.loadDeployment(functionID)
		if err != nil {
			l.logger.ErrorContext(ctx, "load local function deployment", attr.SlogError(err), attr.SlogDeploymentFunctionsID(functionID.String()))
			http.Error(w, "local function deployment not found", http.StatusNotFound)
			return
		}

		resp, err := l.invokeLocalRunner(ctx, functionID, deployment, action, r)
		if err != nil {
			l.logger.ErrorContext(ctx, "invoke local function runner", attr.SlogError(err), attr.SlogDeploymentFunctionsID(functionID.String()))
			http.Error(w, "local function invocation failed", http.StatusBadGateway)
			return
		}
		defer o11y.LogDefer(ctx, l.logger, func() error { return resp.Body.Close() })

		for key, values := range resp.Header {
			if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		trailerKeys := make([]string, 0, len(resp.Trailer))
		for key := range resp.Trailer {
			trailerKeys = append(trailerKeys, key)
		}
		slices.Sort(trailerKeys)
		if len(trailerKeys) > 0 {
			w.Header().Set("Trailer", strings.Join(trailerKeys, ", "))
		}

		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			l.logger.ErrorContext(ctx, "copy local runner response body", attr.SlogError(err), attr.SlogDeploymentFunctionsID(functionID.String()))
		}

		for _, key := range trailerKeys {
			for _, value := range resp.Trailer.Values(key) {
				w.Header().Add(key, value)
			}
		}
	}
}

func (l *LocalRunner) invokeLocalRunner(ctx context.Context, functionID uuid.UUID, deployment localRunnerDeployment, action string, incoming *http.Request) (*http.Response, error) {
	runnerBinary, err := l.ensureRunnerBinary(ctx)
	if err != nil {
		return nil, err
	}

	workDir, err := os.MkdirTemp(l.functionDir(functionID), "work-")
	if err != nil {
		return nil, fmt.Errorf("create local runner work dir: %w", err)
	}

	addrFileHandle, err := os.CreateTemp(l.functionDir(functionID), "runner-addr-*.txt")
	if err != nil {
		makeWritableRecursive(workDir)
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("create local runner address file: %w", err)
	}
	addrFile := addrFileHandle.Name()
	if err := addrFileHandle.Close(); err != nil {
		makeWritableRecursive(workDir)
		_ = os.RemoveAll(workDir)
		_ = os.Remove(addrFile)
		return nil, fmt.Errorf("close local runner address file: %w", err)
	}

	env := append(os.Environ(),
		"GRAM_SERVER_URL="+l.serverURL.String(),
		functionAuthSecretVar+"="+deployment.BearerSecret,
		"GRAM_PROJECT_ID="+deployment.ProjectID,
		"GRAM_DEPLOYMENT_ID="+deployment.DeploymentID,
		"GRAM_FUNCTION_ID="+deployment.FunctionID,
	)

	codePath := filepath.Join(l.functionDir(functionID), localRunnerCodeZipName)
	language, err := localRuntimeLanguage(deployment.Runtime)
	if err != nil {
		makeWritableRecursive(workDir)
		_ = os.RemoveAll(workDir)
		_ = os.Remove(addrFile)
		return nil, err
	}

	if err := l.runLocalRunnerInit(ctx, runnerBinary, env, language, codePath, workDir); err != nil {
		makeWritableRecursive(workDir)
		_ = os.RemoveAll(workDir)
		_ = os.Remove(addrFile)
		return nil, err
	}

	proxyCtx, cancel := context.WithCancel(ctx)

	//nolint:gosec // command and arguments are derived from trusted local runner paths and validated runtime metadata.
	serverCmd := exec.CommandContext(proxyCtx, runnerBinary,
		"-language", language,
		"-codePath", codePath,
		"-workDir", workDir,
		"-addr", "127.0.0.1:0",
		"-addr-file", addrFile,
	)
	serverCmd.Env = env
	serverOutput := &bytes.Buffer{}
	serverCmd.Stdout = serverOutput
	serverCmd.Stderr = serverOutput
	if err := serverCmd.Start(); err != nil {
		cancel()
		makeWritableRecursive(workDir)
		_ = os.RemoveAll(workDir)
		_ = os.Remove(addrFile)
		return nil, fmt.Errorf("start local runner server: %w", err)
	}

	cleanup := localRunnerCleanup(ctx, l.logger, functionID, workDir, addrFile, cancel, serverCmd)

	runnerAddr, err := waitForLocalRunnerAddrFile(proxyCtx, addrFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for local runner address file: %w\n%s", err, serverOutput.String())
	}

	if err := waitForLocalRunner(proxyCtx, l.httpClient, "http://"+runnerAddr+"/healthz"); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for local runner startup: %w\n%s", err, serverOutput.String())
	}

	body, err := io.ReadAll(incoming.Body)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read proxied request body: %w", err)
	}

	endpoint := "http://" + runnerAddr + "/" + action
	outReq, err := http.NewRequestWithContext(ctx, incoming.Method, endpoint, bytes.NewReader(body))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create proxied request: %w", err)
	}
	outReq.Header = incoming.Header.Clone()

	resp, err := l.httpClient.Do(outReq)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("execute proxied request: %w\n%s", err, serverOutput.String())
	}
	resp.Body = &localRunnerResponseBody{
		ReadCloser: resp.Body,
		cleanup:    cleanup,
	}

	return resp, nil
}

func localRunnerCleanup(
	ctx context.Context,
	logger *slog.Logger,
	functionID uuid.UUID,
	workDir string,
	addrFile string,
	cancel context.CancelFunc,
	serverCmd *exec.Cmd,
) func() {
	var once sync.Once

	return func() {
		once.Do(func() {
			cancel()
			_ = serverCmd.Wait()
			if rmErr := os.Remove(addrFile); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				logger.ErrorContext(ctx, "remove local runner address file", attr.SlogError(rmErr), attr.SlogDeploymentFunctionsID(functionID.String()))
			}
			makeWritableRecursive(workDir)
			if rmErr := os.RemoveAll(workDir); rmErr != nil {
				logger.ErrorContext(ctx, "remove local runner work dir", attr.SlogError(rmErr), attr.SlogDeploymentFunctionsID(functionID.String()))
			}
		})
	}
}

type localRunnerResponseBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *localRunnerResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cleanup()
	if err != nil {
		return fmt.Errorf("close local runner response body: %w", err)
	}
	return nil
}

func (l *LocalRunner) runLocalRunnerInit(ctx context.Context, runnerBinary string, env []string, language string, codePath string, workDir string) error {
	//nolint:gosec // command and arguments are derived from trusted local runner paths and validated runtime metadata.
	initCmd := exec.CommandContext(ctx, runnerBinary,
		"-init",
		"-language", language,
		"-codePath", codePath,
		"-workDir", workDir,
	)
	initCmd.Env = env
	output, err := initCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("initialize local runner: %w\n%s", err, string(output))
	}

	return nil
}

func (l *LocalRunner) ensureRunnerBinary(ctx context.Context) (string, error) {
	l.binaryOnce.Do(func() {
		if err := os.MkdirAll(l.codeRootDir, 0750); err != nil {
			l.binaryErr = fmt.Errorf("create local functions root: %w", err)
			return
		}

		binDir := filepath.Join(l.codeRootDir, ".bin")
		if err := os.MkdirAll(binDir, 0750); err != nil {
			l.binaryErr = fmt.Errorf("create local functions bin dir: %w", err)
			return
		}

		l.binaryPath = filepath.Join(binDir, localRunnerBinaryName)
		//nolint:gosec // builds the checked-out local runner from a fixed repo-relative package path.
		cmd := exec.CommandContext(ctx, "go", "build", "-o", l.binaryPath, "./functions/cmd/runner")
		cmd.Dir = localFunctionsRepoRoot()
		output, err := cmd.CombinedOutput()
		if err != nil {
			l.binaryErr = fmt.Errorf("build local runner binary: %w\n%s", err, string(output))
			return
		}
	})

	return l.binaryPath, l.binaryErr
}

func (l *LocalRunner) copyAsset(ctx context.Context, assetURL *url.URL, dst string) error {
	src, err := l.assetStore.Read(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("open asset: %w", err)
	}
	defer o11y.LogDefer(ctx, l.logger, func() error { return src.Close() })

	tmpPath := dst + ".tmp"
	//nolint:gosec // temporary destination lives under the orchestrator-managed local function directory.
	dstFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open asset destination: %w", err)
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		_ = o11y.LogDefer(ctx, l.logger, func() error { return dstFile.Close() })
	}()

	if _, err := io.Copy(dstFile, src); err != nil {
		return fmt.Errorf("copy asset contents: %w", err)
	}
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close asset destination: %w", err)
	}
	closed = true
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename copied asset: %w", err)
	}

	return nil
}

func (l *LocalRunner) loadDeployment(functionID uuid.UUID) (localRunnerDeployment, error) {
	var deployment localRunnerDeployment

	metadataPath := filepath.Join(l.functionDir(functionID), localRunnerMetadataName)
	// #nosec G304 -- metadata path is constrained to the local functions root and a UUID-derived directory.
	bs, err := os.ReadFile(metadataPath)
	if err != nil {
		return deployment, fmt.Errorf("read local deployment metadata: %w", err)
	}
	if err := json.Unmarshal(bs, &deployment); err != nil {
		return deployment, fmt.Errorf("decode local deployment metadata: %w", err)
	}

	return deployment, nil
}

func (l *LocalRunner) functionDir(functionID uuid.UUID) string {
	return filepath.Join(l.codeRootDir, functionID.String())
}

func invCheckLocalDeploy(req RunnerDeployRequest) error {
	if err := inv.Check(
		"local function deploy request",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"function id cannot be nil", req.FunctionID != uuid.Nil,
		"access id cannot be nil", req.AccessID != uuid.Nil,
		"runtime must be supported", func() error {
			if IsSupportedRuntime(string(req.Runtime)) {
				return nil
			}
			return fmt.Errorf("unsupported runtime: %s", req.Runtime)
		},
		"deployment assets cannot be empty", len(req.Assets) > 0,
		"deployment asset url cannot be nil", func() bool {
			return len(req.Assets) > 0 && req.Assets[0].AssetURL != nil
		},
		"bearer secret cannot be empty", req.BearerSecret != "",
	); err != nil {
		return fmt.Errorf("check local function deploy request: %w", err)
	}

	return nil
}

func invCheckLocalToolCall(req RunnerToolCallRequest) error {
	if err := inv.Check(
		"local function tool call request",
		"organization id cannot be empty", req.OrganizationID != "",
		"organization slug cannot be empty", req.OrganizationSlug != "",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"functions id cannot be nil", req.FunctionsID != uuid.Nil,
		"tool urn cannot be empty", !req.ToolURN.IsZero(),
		"tool name cannot be empty", req.ToolName != "",
	); err != nil {
		return fmt.Errorf("check local function tool call request: %w", err)
	}

	return nil
}

func invCheckLocalReadResource(req RunnerResourceReadRequest) error {
	if err := inv.Check(
		"local function read resource request",
		"organization id cannot be empty", req.OrganizationID != "",
		"organization slug cannot be empty", req.OrganizationSlug != "",
		"project id cannot be nil", req.ProjectID != uuid.Nil,
		"deployment id cannot be nil", req.DeploymentID != uuid.Nil,
		"functions id cannot be nil", req.FunctionsID != uuid.Nil,
		"resource urn cannot be empty", !req.ResourceURN.IsZero(),
		"resource uri cannot be empty", req.ResourceURI != "",
	); err != nil {
		return fmt.Errorf("check local function read resource request: %w", err)
	}

	return nil
}

func localRuntimeLanguage(runtime Runtime) (string, error) {
	switch runtime {
	case RuntimeNodeJS22, RuntimeNodeJS24:
		return "javascript", nil
	case RuntimePython312:
		return "python", nil
	default:
		return "", fmt.Errorf("unsupported runtime: %s", runtime)
	}
}

func waitForLocalRunner(ctx context.Context, client *guardian.HTTPClient, healthURL string) error {
	deadline := time.Now().Add(localRunnerHealthTimeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("create local runner health request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("local runner startup canceled: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	return fmt.Errorf("timed out waiting for local runner health endpoint")
}

func localFunctionsRepoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func waitForLocalRunnerAddrFile(ctx context.Context, path string) (string, error) {
	deadline := time.Now().Add(localRunnerHealthTimeout)
	for time.Now().Before(deadline) {
		// #nosec G304 -- address file path is orchestrator-controlled inside the temporary work directory.
		bs, err := os.ReadFile(path)
		switch {
		case err == nil:
			addr := strings.TrimSpace(string(bs))
			if addr == "" {
				break
			}
			return addr, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return "", fmt.Errorf("read local runner address file: %w", err)
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("local runner address discovery canceled: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}

	return "", fmt.Errorf("timed out waiting for local runner address file")
}

func writeJSONAtomic[T any](path string, value T, mode os.FileMode) error {
	bs, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, bs, mode); err != nil {
		return fmt.Errorf("write json tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename json tmp file: %w", err)
	}

	return nil
}

func makeWritableRecursive(root string) {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer func() {
		_ = rootFS.Close()
	}()

	_ = fs.WalkDir(rootFS.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// #nosec G302 -- local cleanup only needs owner/group write to ensure temp dirs can be removed.
			_ = rootFS.Chmod(path, 0750)
			return nil
		}
		// #nosec G302 -- local cleanup only needs owner/group write to ensure temp files can be removed.
		_ = rootFS.Chmod(path, 0640)
		return nil
	})
}
