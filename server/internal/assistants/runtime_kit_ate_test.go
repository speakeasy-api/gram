package assistants

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestKitATEActorNamesBindThreadAndGeneration(t *testing.T) {
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
