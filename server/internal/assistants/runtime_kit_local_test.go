package assistants

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

type fakeKitLocalEngine struct {
	info      kitLocalContainerInfo
	volume    map[string]string
	calls     []string
	spec      kitLocalContainerSpec
	bootstrap KitLocalBootstrap
	failCopy  bool
	nextPort  int
}

func (f *fakeKitLocalEngine) ImageID(context.Context, string) (string, error) {
	return "sha256:test-image", nil
}
func (f *fakeKitLocalEngine) InspectKit(context.Context, string) (kitLocalContainerInfo, error) {
	if f.info.ID == "" {
		return kitLocalContainerInfo{}, errLocalContainerNotFound
	}
	return f.info, nil
}
func (f *fakeKitLocalEngine) InspectKitVolume(context.Context, string) (map[string]string, error) {
	if f.volume == nil {
		return nil, errLocalContainerNotFound
	}
	return f.volume, nil
}
func (f *fakeKitLocalEngine) CreateKitVolume(_ context.Context, _ string, labels map[string]string) error {
	f.calls = append(f.calls, "volume")
	f.volume = maps.Clone(labels)
	return nil
}
func (f *fakeKitLocalEngine) CreateKit(_ context.Context, spec kitLocalContainerSpec) (string, error) {
	f.calls = append(f.calls, "create")
	f.spec = spec
	f.info = kitLocalContainerInfo{ID: "container-id", ImageID: "sha256:test-image", User: "1000:1000", HomeVolume: spec.VolumeName, Labels: maps.Clone(spec.Labels)}
	return f.info.ID, nil
}
func (f *fakeKitLocalEngine) CopyKitBootstrap(_ context.Context, _ string, b KitLocalBootstrap) error {
	f.calls = append(f.calls, "copy")
	if f.failCopy {
		return errors.New("injection failed")
	}
	f.bootstrap = b
	return nil
}
func (f *fakeKitLocalEngine) Start(context.Context, string) error {
	f.calls = append(f.calls, "start")
	f.info.Running = true
	f.info.HostPort = f.nextPort
	return nil
}
func (f *fakeKitLocalEngine) Stop(context.Context, string) error {
	f.calls = append(f.calls, "stop")
	f.info.Running = false
	f.info.HostPort = 0
	return nil
}
func (f *fakeKitLocalEngine) Remove(context.Context, string) error {
	f.calls = append(f.calls, "remove")
	f.info = kitLocalContainerInfo{}
	return nil
}
func (f *fakeKitLocalEngine) RemoveVolume(context.Context, string) error {
	f.calls = append(f.calls, "remove-volume")
	f.volume = nil
	return nil
}

type fakeKitLocalProtocol struct{ turns, interrupts, statuses int }

func (p *fakeKitLocalProtocol) RunTurn(context.Context, assistantRuntimeRecord, runTurnRequest) error {
	p.turns++
	return nil
}
func (p *fakeKitLocalProtocol) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	p.interrupts++
	return true, nil
}
func (p *fakeKitLocalProtocol) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	p.statuses++
	return RuntimeBackendStatus{Configured: true}, nil
}

func kitLocalFixture(t *testing.T) (*KitLocalRuntimeBackend, *fakeKitLocalEngine, assistantRuntimeRecord, *fakeKitLocalProtocol) {
	t.Helper()
	engine := &fakeKitLocalEngine{nextPort: 32100}
	protocol := &fakeKitLocalProtocol{}
	config := KitLocalRuntimeConfig{Environment: "local", Image: "example.invalid/kit:local", Command: []string{"/usr/local/bin/kit"}, Args: func() []string { _, args := KitGatewayCommand(8080); return args }(), GuestPort: 8080, ServerURL: &url.URL{Scheme: "https", Host: "host.docker.internal:8443"}, CompatibilityGeneration: "local-generation-one", HomeOwnedByRuntimeUser: true,
		Bootstrap: func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{AgentsMD: []byte("caller instructions"), InferenceJSON: []byte(`{"version":1,"bearer_token":"sai_test","origin":"https://host.docker.internal:8443"}`), MCPJSON: []byte(`{"mcpServers":{}}`), GatewayToken: []byte("private-test-token")}, nil
		},
		Protocol: func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) { return protocol, nil },
		Ready: func(context.Context, assistantRuntimeRecord, *url.URL) error {
			engine.calls = append(engine.calls, "ready")
			return nil
		},
	}
	backend, err := newKitLocalRuntimeBackend(config, engine)
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), RuntimeVersion: 3, Backend: "local"}
	return backend, engine, r, protocol
}
func TestKitLocalLifecycleAndEndpointRediscovery(t *testing.T) {
	t.Parallel()
	k, engine, r, protocol := kitLocalFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	require.Equal(t, []string{"volume", "create", "copy", "start", "ready"}, engine.calls)
	require.Equal(t, "private-test-token", string(engine.bootstrap.GatewayToken))
	require.True(t, strings.HasPrefix(engine.spec.Name, "gram-kit-"))
	require.NotContains(t, engine.spec.VolumeName, "gram-asst")
	r.BackendMetadataJSON = result.BackendMetadataJSON
	var endpoint string
	k.config.Protocol = func(_ context.Context, _ assistantRuntimeRecord, u *url.URL) (runtimeProtocol, error) {
		endpoint = u.String()
		return protocol, nil
	}
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{}))
	require.Equal(t, "http://127.0.0.1:32100/acp/v2", endpoint)
	require.NoError(t, k.Stop(t.Context(), r))
	require.NotNil(t, engine.volume)
	engine.nextPort = 32200
	result, err = k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.Contains(t, string(result.BackendMetadataJSON), "32200")
	// Protocol resolves the current port even when persisted metadata is stale.
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{}))
	require.Equal(t, "http://127.0.0.1:32200/acp/v2", endpoint)
	interrupted, err := k.InterruptTurn(t.Context(), r, uuid.New())
	require.NoError(t, err)
	require.True(t, interrupted)
	status, err := k.Status(t.Context(), r)
	require.NoError(t, err)
	require.True(t, status.Configured)
	require.Equal(t, 2, protocol.turns)
	require.Equal(t, 1, protocol.interrupts)
	require.Equal(t, 1, protocol.statuses)
	require.Error(t, k.ReapStoppedMachine(t.Context(), r))
	require.NoError(t, k.Stop(t.Context(), r))
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	require.Nil(t, engine.volume)
	require.Empty(t, engine.info.ID)
	require.NoError(t, k.Reap(t.Context(), r))
}
func TestKitLocalReadinessAndInjectionAreRequired(t *testing.T) {
	t.Parallel()
	t.Run("injection", func(t *testing.T) {
		t.Parallel()
		k, engine, r, _ := kitLocalFixture(t)
		engine.failCopy = true
		_, err := k.Ensure(t.Context(), r)
		require.Error(t, err)
		require.NotContains(t, engine.calls, "start")
		require.NotContains(t, engine.calls, "ready")
	})
	t.Run("authenticated readiness", func(t *testing.T) {
		t.Parallel()
		k, _, r, _ := kitLocalFixture(t)
		k.config.Ready = func(context.Context, assistantRuntimeRecord, *url.URL) error { return errors.New("unauthorized") }
		result, err := k.Ensure(t.Context(), r)
		require.Error(t, err)
		require.Empty(t, result.BackendMetadataJSON)
	})
}
func TestKitLocalRejectsForeignAuthority(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"version", "assistant-label", "generation-label", "runtime-label", "volume-label", "saved-version", "saved-generation", "user", "mount", "container-id"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			k, engine, r, _ := kitLocalFixture(t)
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			engine.calls = nil
			switch field {
			case "version":
				r.RuntimeVersion = 2
			case "assistant-label":
				engine.info.Labels["gram.ai/assistant-id"] = uuid.NewString()
			case "generation-label":
				engine.info.Labels["gram.ai/compatibility-generation"] = "other"
			case "runtime-label":
				engine.info.Labels["gram.ai/runtime-lineage"] = uuid.NewString()
			case "volume-label":
				engine.volume["gram.ai/assistant-id"] = uuid.NewString()
			case "saved-version":
				r.BackendMetadataJSON = bytes.ReplaceAll(r.BackendMetadataJSON, []byte(`"runtime_version":3`), []byte(`"runtime_version":2`))
			case "saved-generation":
				r.BackendMetadataJSON = bytes.ReplaceAll(r.BackendMetadataJSON, []byte("local-generation-one"), []byte("other"))
			case "user":
				engine.info.User = "0"
			case "mount":
				engine.info.HomeVolume = "foreign-home"
			case "container-id":
				engine.info.ID = "replacement-container"
			}
			_, err = k.Ensure(t.Context(), r)
			require.Error(t, err)
			require.Error(t, k.RunTurn(t.Context(), r, runTurnRequest{}))
			require.Error(t, k.Reap(t.Context(), r))
			require.Empty(t, engine.calls)
		})
	}
}
func TestKitLocalDoesNotAdoptLegacyOrCrossAssistantVolume(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	engine.volume = map[string]string{"gram.ai/runtime-version": "2"}
	_, err := k.Ensure(t.Context(), r)
	require.Error(t, err)
	require.Empty(t, engine.calls)
	first, volume := kitLocalNames(r)
	r.ID = uuid.New()
	other, otherVolume := kitLocalNames(r)
	require.NotEqual(t, first, other)
	require.NotEqual(t, volume, otherVolume)
}
func TestKitLocalConstructorRequiresExplicitContracts(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"environment", "bootstrap", "protocol", "ready", "ownership", "command", "args", "generation", "home"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			k, engine, _, _ := kitLocalFixture(t)
			config := k.config
			switch field {
			case "environment":
				config.Environment = "production"
			case "bootstrap":
				config.Bootstrap = nil
			case "protocol":
				config.Protocol = nil
			case "ready":
				config.Ready = nil
			case "ownership":
				config.HomeOwnedByRuntimeUser = false
			case "command":
				config.Command = nil
			case "args":
				config.Args = nil
			case "generation":
				config.CompatibilityGeneration = ""
			case "home":
				config.Env = map[string]string{"HOME": "/root"}
			}
			_, err := newKitLocalRuntimeBackend(config, engine)
			require.Error(t, err)
		})
	}
}
func TestKitLocalBootstrapTarPermissionsAndExactBytes(t *testing.T) {
	t.Parallel()
	k, _, r, _ := kitLocalFixture(t)
	b, err := k.config.Bootstrap(t.Context(), r)
	require.NoError(t, err)
	archive, err := kitLocalBootstrapArchive(b)
	require.NoError(t, err)
	reader := tar.NewReader(bytes.NewReader(archive))
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		require.Equal(t, 1000, header.Uid)
		require.Equal(t, 1000, header.Gid)
		if header.Typeflag == tar.TypeDir {
			require.Equal(t, int64(0700), header.Mode)
			continue
		}
		require.Equal(t, int64(0600), header.Mode)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		files[header.Name] = data
	}
	require.Equal(t, map[string][]byte{".kit/AGENTS.md": b.AgentsMD, ".kit/config.toml": []byte(kitFileConfig), ".kit/credentials/" + KitInferenceCredentialFilename: b.InferenceJSON, ".kit/credentials/mcp.json": b.MCPJSON, ".kit/credentials/gateway-token": b.GatewayToken}, files)
}
func TestKitLocalDockerCreateUsesOnlyInjectedImageContract(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	_, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	spec := engine.spec
	spec.ExtraCACertFile = "/public-ca.pem"
	args := kitLocalCreateArgs(8080, spec)
	joined := strings.Join(args, " ")
	require.Equal(t, "create", args[0])
	require.Contains(t, joined, "--user 1000:1000")
	require.Contains(t, joined, "127.0.0.1::8080")
	require.Contains(t, joined, "host.docker.internal:host-gateway")
	require.Contains(t, joined, ":/home/kit")
	require.Contains(t, joined, "--entrypoint /usr/local/bin/kit example.invalid/kit:local gateway")
	require.Contains(t, joined, "SSL_CERT_FILE="+localRuntimeCACertMountPath)
	require.Contains(t, joined, "NODE_EXTRA_CA_CERTS="+localRuntimeCACertMountPath)
	require.NotContains(t, joined, "private-test-token")
	require.NotContains(t, joined, "/healthz")
	require.NotContains(t, joined, "/state")
}
func TestKitLocalDockerInspectRejectsPublicPort(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"Id":"id","Image":"image","Config":{"User":"1000:1000","Labels":{}},"State":{"Running":true},"Mounts":[{"Type":"volume","Name":"home","Destination":"/home/kit"}],"NetworkSettings":{"Ports":{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"33000"}]}}}`)
	info, err := decodeKitLocalInspect(raw, 8080)
	require.NoError(t, err)
	require.Equal(t, 33000, info.HostPort)
	require.Equal(t, "home", info.HomeVolume)
	_, err = decodeKitLocalInspect(bytes.ReplaceAll(raw, []byte("127.0.0.1"), []byte("0.0.0.0")), 8080)
	require.Error(t, err)
}

func TestKitLocalReservationMetadataSentinel(t *testing.T) {
	for _, raw := range []string{"", "{}", "{ \n\t }"} {
		t.Run(fmt.Sprintf("empty-%q", raw), func(t *testing.T) {
			backend, engine, r, _ := kitLocalFixture(t)
			r.BackendMetadataJSON = []byte(raw)
			result, err := backend.Ensure(t.Context(), r)
			require.NoError(t, err)
			require.True(t, result.ColdStart)
			require.NotEmpty(t, result.BackendMetadataJSON)
			// An interrupted first ensure can leave a correctly labelled guest before
			// metadata persistence. The same sentinel must not reject that owned guest.
			_, err = backend.Ensure(t.Context(), r)
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(strings.Join(engine.calls, ","), "create"))
		})
	}
	for _, raw := range []string{"null", "[]", "[{}]", `{"runtime_version":3}`, `{"unknown":true}`, "{"} {
		t.Run("reject-"+raw, func(t *testing.T) {
			backend, engine, r, _ := kitLocalFixture(t)
			r.BackendMetadataJSON = []byte(raw)
			_, err := backend.Ensure(t.Context(), r)
			require.Error(t, err)
			require.Empty(t, engine.calls, "invalid persisted authority must fail before infrastructure creation")
		})
	}
}

func TestKitLocalEnsureAcceptsDatabaseReservation(t *testing.T) {
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_reservation_metadata")
	require.NoError(t, err)
	assistant, project := seedAssistant(t, db, "kit-reservation-metadata", 1)
	thread := seedThreadWithEvent(t, db, assistant, "kit-reservation-metadata", "kit-reservation-metadata", eventStatusPending)
	_, err = db.Exec(t.Context(), `UPDATE assistants SET runtime_version=3 WHERE project_id=$1 AND id=$2`, project, assistant)
	require.NoError(t, err)
	queries := assistantrepo.New(db)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant, AssistantThreadID: thread, Backend: runtimeBackendLocal, State: runtimeStateStarting}))
	row, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant})
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(row.BackendMetadataJson))
	backend, _, _, _ := kitLocalFixture(t)
	record := assistantRuntimeRecord{ID: row.ID, ProjectID: row.ProjectID, AssistantID: row.AssistantID, AssistantThreadID: row.AssistantThreadID, Backend: row.Backend, RuntimeVersion: row.RuntimeVersion, BackendMetadataJSON: row.BackendMetadataJson}
	result, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	record.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, backend.validate(record))
	record.ID = uuid.New()
	require.Error(t, backend.validate(record), "a different runtime generation cannot inherit saved authority")
}
