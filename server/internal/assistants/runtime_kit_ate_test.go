package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestKitATEActorNamesBindThreadAndGeneration(t *testing.T) {
	t.Parallel()
	r := assistantRuntimeRecord{ID: uuid.New()}
	thread := uuid.New()
	name := kitATEActorName(r, thread)
	if len(name) > 63 || !kitATEPreviewIDPattern.MatchString(name) {
		t.Fatal("invalid actor name")
	}
	if name != kitATEActorName(r, thread) {
		t.Fatal("actor identity changed within generation")
	}
	if name == kitATEActorName(r, uuid.New()) {
		t.Fatal("threads share an actor")
	}
	r.ID = uuid.New()
	if name == kitATEActorName(r, thread) {
		t.Fatal("actor name reused across generations")
	}
}

func TestKitATEMetadataRejectsForeignBinding(t *testing.T) {
	t.Parallel()
	k := &KitATERuntimeBackend{config: KitATERuntimeConfig{PreviewID: "preview-one", Image: "fixture", Atespace: "preview-one", Template: "kit"}}
	r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), Backend: runtimeBackendATE, RuntimeVersion: 3}
	m, err := k.metadata(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	r.BackendMetadataJSON = raw
	if _, err := k.metadata(r); err != nil {
		t.Fatal(err)
	}
	r.ProjectID = uuid.New()
	if _, err := k.metadata(r); !errors.Is(err, ErrKitRuntimeBinding) {
		t.Fatal("foreign metadata was accepted")
	}
}

func TestKitATETrustedEgressHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"api.example.test", "ai.speakeasy.com"} {
		if !kitATETrustedDNSHost(host) {
			t.Errorf("trusted DNS host rejected: %s", host)
		}
	}
	for _, host := range []string{"169.254.169.254", "127.0.0.1", "[::1]", "*.example.test", "localhost", "example.test/path", "example.test:443", "user@example.test"} {
		if kitATETrustedDNSHost(host) {
			t.Errorf("unsafe egress host accepted: %s", host)
		}
	}
}

func TestKitATEStoppedTTLRetainsReusableActor(t *testing.T) {
	t.Parallel()
	calls := 0
	k := &KitATERuntimeBackend{config: KitATERuntimeConfig{Lifecycle: func(ctx context.Context, r assistantRuntimeRecord, operation func(context.Context, assistantRuntimeRecord) error) error {
		calls++
		return operation(ctx, r)
	}}}
	err := k.ReapStoppedMachine(t.Context(), assistantRuntimeRecord{Backend: runtimeBackendATE, RuntimeVersion: 3})
	if err == nil || !strings.Contains(err.Error(), "must not be reaped by TTL") || calls != 1 {
		t.Fatal("TTL cleanup did not preserve the reusable actor")
	}
}
