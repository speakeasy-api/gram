package assistants

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const runtimeBackendATE = "ate"

type kitATEControl interface {
	Invoke(context.Context, string, json.RawMessage) (json.RawMessage, error)
	ActorHTTPClient(string, uint16) (*http.Client, error)
}

type KitATERuntimeConfig struct {
	Revoke                                            func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error
	CheckSettled                                      func(context.Context, assistantRuntimeRecord) error
	Environment, PreviewID, Image, Atespace, Template string
	ServerURL                                         *url.URL
	Client                                            kitATEControl
	Bootstrap                                         func(context.Context, assistantRuntimeRecord, uuid.UUID, string) (KitLocalBootstrap, error)
	Token                                             func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error)
	Protocol                                          func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *http.Client, string) (runtimeProtocol, error)
	Lifecycle                                         func(context.Context, assistantRuntimeRecord, func(context.Context, assistantRuntimeRecord) error) error
}

type KitATERuntimeBackend struct{ config KitATERuntimeConfig }

type kitATEThread struct {
	ActorName   string              `json:"actor_name"`
	ActorUID    string              `json:"actor_uid"`
	Workload    *KitWorkloadBinding `json:"workload,omitempty"`
	Configured  bool                `json:"configured"`
	SnapshotURI string              `json:"snapshot_uri,omitempty"`
}

type kitATEMetadata struct {
	RuntimeID   uuid.UUID               `json:"runtime_id"`
	AssistantID uuid.UUID               `json:"assistant_id"`
	ProjectID   uuid.UUID               `json:"project_id"`
	PreviewID   string                  `json:"preview_id"`
	Image       string                  `json:"image"`
	Atespace    string                  `json:"atespace"`
	Template    string                  `json:"template"`
	Threads     map[string]kitATEThread `json:"threads"`
}

type kitATEActor struct {
	Metadata struct {
		Atespace string `json:"atespace"`
		Name     string `json:"name"`
		UID      string `json:"uid"`
	} `json:"metadata"`
	ActorTemplate struct {
		Atespace string `json:"atespace"`
		Name     string `json:"name"`
	} `json:"actor_template"`
	Status struct {
		State          string `json:"state"`
		LastGeneration int32  `json:"last_assigned_generation"`
		Snapshots      []struct {
			Generation int32  `json:"generation"`
			Fidelity   string `json:"fidelity"`
			Durable    struct {
				State  string `json:"state"`
				Object struct {
					URI string `json:"snapshot_uri"`
				} `json:"object"`
			} `json:"durable_snapshot"`
		} `json:"snapshots"`
	} `json:"status"`
}

var _ RuntimeBackend = (*KitATERuntimeBackend)(nil)

func NewKitATERuntimeBackend(c KitATERuntimeConfig) (*KitATERuntimeBackend, error) {
	if err := ValidateKitATEPreview(c.Environment, true, c.PreviewID); err != nil {
		return nil, err
	}
	if !kitGKEExplicitImage(c.Image) || !strings.Contains(c.Image, "@sha256:") || !kitATEPreviewIDPattern.MatchString(c.Atespace) || !kitATEPreviewIDPattern.MatchString(c.Template) || c.ServerURL == nil || c.Client == nil || c.Bootstrap == nil || c.Token == nil || c.Protocol == nil || c.Lifecycle == nil || c.Revoke == nil || c.CheckSettled == nil {
		return nil, errors.New("ATE preview requires a pinned image, actor scope, authenticated client and lifecycle callbacks")
	}
	return &KitATERuntimeBackend{config: c}, nil
}

func (k *KitATERuntimeBackend) Backend() string               { return runtimeBackendATE }
func (k *KitATERuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendATE }
func (k *KitATERuntimeBackend) ImageRef() string              { return k.config.Image }
func (k *KitATERuntimeBackend) ServerURL() *url.URL           { u := *k.config.ServerURL; return &u }

func kitATEActorName(r assistantRuntimeRecord, thread uuid.UUID) string {
	sum := sha256.Sum256([]byte(thread.String()))
	return "spk-kit-" + strings.ReplaceAll(r.ID.String(), "-", "") + "-" + hex.EncodeToString(sum[:8])
}

func (k *KitATERuntimeBackend) metadata(r assistantRuntimeRecord) (kitATEMetadata, error) {
	var m kitATEMetadata
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return m, ErrKitRuntimeBinding
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return kitATEMetadata{RuntimeID: r.ID, AssistantID: r.AssistantID, ProjectID: r.ProjectID, PreviewID: k.config.PreviewID, Image: k.config.Image, Atespace: k.config.Atespace, Template: k.config.Template, Threads: map[string]kitATEThread{}}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.PreviewID != k.config.PreviewID || m.Atespace != k.config.Atespace || m.Template != k.config.Template || m.Image != k.config.Image || m.Threads == nil {
		return m, ErrKitRuntimeBinding
	}
	for id, t := range m.Threads {
		thread, err := uuid.Parse(id)
		if err != nil || thread == uuid.Nil || t.ActorName != kitATEActorName(r, thread) || t.ActorUID == "" {
			return m, ErrKitRuntimeBinding
		}
	}
	return m, nil
}

func saveKitATEMetadata(ctx context.Context, m kitATEMetadata) error {
	write, ok := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter)
	if !ok {
		return errors.New("ATE actor mutation requires a durable lifecycle writer")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return write(ctx, raw)
}

func (k *KitATERuntimeBackend) call(ctx context.Context, method string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	result, err := k.config.Client.Invoke(ctx, method, raw)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(result, out); err != nil {
		return errors.New("invalid ATE response")
	}
	return nil
}
func (k *KitATERuntimeBackend) ref(name string) map[string]string {
	return map[string]string{"atespace": k.config.Atespace, "name": name}
}
func (k *KitATERuntimeBackend) get(ctx context.Context, t kitATEThread) (kitATEActor, error) {
	var actor kitATEActor
	err := k.call(ctx, "GetActor", map[string]any{"actor": k.ref(t.ActorName)}, &actor)
	if err != nil {
		return actor, err
	}
	if actor.Metadata.Atespace != k.config.Atespace || actor.Metadata.Name != t.ActorName || actor.Metadata.UID == "" || (t.ActorUID != "" && actor.Metadata.UID != t.ActorUID) || actor.ActorTemplate.Atespace != k.config.Atespace || actor.ActorTemplate.Name != k.config.Template {
		return actor, ErrKitRuntimeBinding
	}
	return actor, nil
}

func (k *KitATERuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	m, err := k.metadata(r)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	if r.Retired {
		return RuntimeBackendEnsureResult{}, ErrKitRuntimeBinding
	}
	if err := k.ensureTemplate(ctx); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	raw, err := json.Marshal(m)
	return RuntimeBackendEnsureResult{ColdStart: len(m.Threads) == 0, BackendMetadataJSON: raw}, err
}

func (k *KitATERuntimeBackend) prepare(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (assistantRuntimeRecord, kitATEThread, string, error) {
	var prepared assistantRuntimeRecord
	var entry kitATEThread
	var token string
	err := k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if locked.Retired {
			return ErrKitRuntimeBinding
		}
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}
		var known bool
		entry, known = m.Threads[thread.String()]
		if !known {
			entry.ActorName = kitATEActorName(locked, thread)
			actor, err := k.get(ctx, entry)
			if status.Code(err) == codes.NotFound {
				err = k.call(ctx, "CreateActor", map[string]any{"actor": map[string]any{"metadata": k.ref(entry.ActorName), "actor_template": k.ref(k.config.Template)}}, &actor)
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if actor.Metadata.UID == "" || actor.Metadata.Name != entry.ActorName || actor.Metadata.Atespace != k.config.Atespace {
				return ErrKitRuntimeBinding
			}
			entry.ActorUID = actor.Metadata.UID
			m.Threads[thread.String()] = entry
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
		} else if _, err := k.get(ctx, entry); err != nil {
			return err
		}
		token, err = k.config.Token(ctx, locked, thread)
		if err != nil {
			return err
		}
		helper, err := k.config.Client.ActorHTTPClient(entry.ActorName, 8082)
		if err != nil {
			return err
		}
		defer helper.CloseIdleConnections()
		base, _ := ateclient.ActorEndpoint(entry.ActorName, 8082)
		base = strings.TrimSuffix(base, "/acp")
		if err := waitKitATEHelper(ctx, helper, base+"/readyz"); err != nil {
			return err
		}
		if !entry.Configured {
			locked.BackendMetadataJSON, err = json.Marshal(m)
			if err != nil {
				return err
			}
			bootstrap, err := k.config.Bootstrap(ctx, locked, thread, entry.ActorName)
			if err != nil {
				return err
			}
			entry.Workload = bootstrap.Workload
			m.Threads[thread.String()] = entry
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
			if err := k.ensureEgress(ctx, entry.ActorName, bootstrap); err != nil {
				return err
			}
			archive, err := kitBootstrapArchive(bootstrap)
			if err != nil {
				return err
			}
			code, err := kitATEPost(ctx, helper, base+"/bootstrap", "", archive)
			if err != nil || (code != http.StatusNoContent && code != http.StatusConflict) {
				return errors.New("ATE Kit bootstrap failed")
			}
		}
		if entry.Workload == nil || entry.Workload.Audience == "" {
			return ErrKitRuntimeBinding
		}
		var jwt struct {
			Token string `json:"actor_jwt"`
		}
		if err := k.call(ctx, "MintActorJwt", map[string]any{"actor": k.ref(entry.ActorName), "audiences": []string{entry.Workload.Audience}, "expiration_seconds": 3600}, &jwt); err != nil {
			return err
		}
		if jwt.Token == "" {
			return errors.New("ATE omitted the actor assertion")
		}
		code, err := kitATEPost(ctx, helper, base+"/assertion", token, []byte(jwt.Token))
		if err != nil || code != http.StatusNoContent {
			return errors.New("ATE actor assertion refresh failed")
		}
		if err := waitKitATEHelper(ctx, helper, base+"/kit-readyz"); err != nil {
			return err
		}
		entry.Configured = true
		m.Threads[thread.String()] = entry
		if err := saveKitATEMetadata(ctx, m); err != nil {
			return err
		}
		raw, _ := json.Marshal(struct {
			kitATEMetadata
			Workload *KitWorkloadBinding `json:"workload"`
		}{m, entry.Workload})
		prepared = locked
		prepared.LifecycleTx = nil
		prepared.BackendMetadataJSON = raw
		return nil
	})
	return prepared, entry, token, err
}

func waitKitATEHelper(ctx context.Context, client *http.Client, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		res, err := client.Do(req)
		if err != nil {
			var connect *ateclient.ConnectError
			if !errors.As(err, &connect) || connect.StatusCode != http.StatusServiceUnavailable {
				return errors.New("ATE initialization connection failed")
			}
		}
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
			if res.StatusCode != http.StatusServiceUnavailable {
				return errors.New("ATE initialization endpoint rejected")
			}
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("ATE initialization readiness timed out")
		case <-timer.C:
		}
	}
}
func kitATEPost(ctx context.Context, c *http.Client, endpoint, token string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, errors.New("ATE initialization request failed")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	return res.StatusCode, nil
}
func (k *KitATERuntimeBackend) protocol(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID, t kitATEThread, token string) (runtimeProtocol, error) {
	client, err := k.config.Client.ActorHTTPClient(t.ActorName, 8081)
	if err != nil {
		return nil, err
	}
	endpoint, err := ateclient.ActorEndpoint(t.ActorName, 8081)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(endpoint)
	return k.config.Protocol(ctx, r, thread, t.ActorName, u, client, token)
}
func (k *KitATERuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	if turn.ThreadID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	prepared, t, token, err := k.prepare(ctx, r, turn.ThreadID)
	if err != nil {
		return err
	}
	p, err := k.protocol(ctx, prepared, turn.ThreadID, t, token)
	if err != nil {
		return err
	}
	return p.RunTurn(ctx, prepared, turn)
}
func (k *KitATERuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	// Interruption is persisted by the core and consumed by the active projector.
	m, err := k.metadata(r)
	if err != nil {
		return false, err
	}
	t, ok := m.Threads[thread.String()]
	if !ok {
		return false, nil
	}
	var interrupted bool
	err = k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		token, err := k.config.Token(ctx, locked, thread)
		if err != nil {
			return err
		}
		p, err := k.protocol(ctx, locked, thread, t, token)
		if err != nil {
			return err
		}
		interrupted, err = p.InterruptTurn(ctx, locked, thread)
		return err
	})
	return interrupted, err
}
func (k *KitATERuntimeBackend) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	_, err := k.metadata(r)
	return RuntimeBackendStatus{Configured: err == nil}, err
}
func (k *KitATERuntimeBackend) RecycleImage(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{}, nil
}
func (k *KitATERuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	return k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}
		if err := k.config.CheckSettled(ctx, locked); err != nil {
			return err
		}
		for id, t := range m.Threads {
			actor, err := k.get(ctx, t)
			if err != nil {
				return err
			}
			if actor.Status.State != "ACTOR_STATE_SUSPENDED" {
				if actor.Status.State != "ACTOR_STATE_RUNNING" {
					return errors.New("ATE actor is not ready for suspension")
				}
				thread, err := uuid.Parse(id)
				if err != nil {
					return ErrKitRuntimeBinding
				}
				token, err := k.config.Token(ctx, locked, thread)
				if err != nil {
					return err
				}
				protocol, err := k.protocol(ctx, locked, thread, t, token)
				if err != nil {
					return err
				}
				settled, ok := protocol.(interface {
					CloseNativeThreadSession(context.Context, assistantRuntimeRecord, uuid.UUID) error
				})
				if !ok {
					return errors.New("ATE native checkpoint settlement is unavailable")
				}
				if err := settled.CloseNativeThreadSession(ctx, locked, thread); err != nil {
					return err
				}
				var result struct {
					Actor kitATEActor `json:"actor"`
				}
				if err := k.call(ctx, "SuspendActor", map[string]any{"actor": k.ref(t.ActorName), "options": map[string]string{"mode": "DURABILITY_MODE_SYNCHRONOUS"}}, &result); err != nil {
					return err
				}
				actor = result.Actor
			}
			uri := ""
			for _, snapshot := range actor.Status.Snapshots {
				if snapshot.Generation == actor.Status.LastGeneration && snapshot.Fidelity == "SNAPSHOT_FIDELITY_ROOTFS" && snapshot.Durable.State == "SNAPSHOT_STORAGE_STATE_COMPLETED" && strings.HasPrefix(snapshot.Durable.Object.URI, "gs://") {
					uri = snapshot.Durable.Object.URI
				}
			}
			if actor.Metadata.UID != t.ActorUID || actor.Status.State != "ACTOR_STATE_SUSPENDED" || uri == "" {
				return errors.New("ATE actor has no completed durable GCS snapshot")
			}
			t.SnapshotURI = uri
			m.Threads[id] = t
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
		}
		return nil
	})
}
func (k *KitATERuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	return k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}
		for _, t := range m.Threads {
			if t.Workload != nil {
				if err := k.config.Revoke(ctx, locked, t.Workload); err != nil {
					return err
				}
			}
			_, err := k.get(ctx, t)
			if status.Code(err) == codes.NotFound {
				continue
			}
			if err != nil {
				return err
			}
			err = k.call(ctx, "DeleteActor", map[string]any{"actor": k.ref(t.ActorName), "options": map[string]string{"uid": t.ActorUID}}, nil)
			if err != nil && status.Code(err) != codes.NotFound {
				return err
			}
		}
		return nil
	})
}
func (k *KitATERuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	return k.Reap(ctx, r)
}

func (k *KitATERuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	m, err := k.metadata(r)
	if err != nil {
		return false, err
	}
	for _, t := range m.Threads {
		_, err := k.get(ctx, t)
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}
