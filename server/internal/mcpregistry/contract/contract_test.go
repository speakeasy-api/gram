package contract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestConformance(t *testing.T) {
	raw, err := os.ReadFile("conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string
		Record json.RawMessage
		Valid  bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	schema, err := Compile(Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			value, err := decode(c.Record)
			if err != nil {
				t.Fatal(err)
			}
			if got := schema.Validate(value) == nil; got != c.Valid {
				t.Fatalf("valid=%v, want %v", got, c.Valid)
			}
		})
	}
}
func TestRejectUnknownFormat(t *testing.T) {
	if _, err := Compile([]byte(`{"type":"string","format":"unsupported-required-format"}`)); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestStarterBaseline(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../baseline/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Count        int
		SchemaSHA256 string `json:"schema_sha256"`
		Records      []struct{ File, Name, SHA256 string }
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Count != 2 || len(manifest.Records) != 2 {
		t.Fatal("expected two starter records")
	}
	if fmt.Sprintf("%x", sha256.Sum256(Schema)) != manifest.SchemaSHA256 {
		t.Fatal("schema hash mismatch")
	}
	files, _ := filepath.Glob("../baseline/records/*.json")
	if len(files) != 2 {
		t.Fatal("unexpected baseline file count")
	}
	schema, err := Compile(Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range manifest.Records {
		raw, err := os.ReadFile(filepath.Join("../baseline", record.File))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != record.SHA256 {
			t.Fatal("record hash mismatch")
		}
		value, err := decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if schema.Validate(value) != nil {
			t.Fatal("starter record invalid")
		}
		if value.(map[string]any)["server"].(map[string]any)["name"] != record.Name {
			t.Fatal("record name mismatch")
		}
	}
}
