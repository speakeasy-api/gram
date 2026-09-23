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
	t.Parallel()
	raw, err := os.ReadFile("conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string          `json:"name"`
		Record json.RawMessage `json:"record"`
		Valid  bool            `json:"valid"`
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
			t.Parallel()
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
	t.Parallel()
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
		Count        int    `json:"count"`
		SchemaSHA256 string `json:"schema_sha256"`
		Records      []struct {
			File   string `json:"file"`
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"records"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Count != 2 || len(manifest.Records) != 2 {
		t.Fatal("expected two starter records")
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(Schema)); got != manifest.SchemaSHA256 {
		t.Fatalf("record.schema.json hash=%s, want %s", got, manifest.SchemaSHA256)
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
		if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != record.SHA256 {
			t.Fatalf("%s hash=%s, want %s", record.File, got, record.SHA256)
		}
		value, err := decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%s invalid: %v", record.File, err)
		}
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatal("record is not an object")
		}
		server, ok := object["server"].(map[string]any)
		if !ok {
			t.Fatal("server is not an object")
		}
		if server["name"] != record.Name {
			t.Fatalf("%s name=%v, want %s", record.File, server["name"], record.Name)
		}
	}
}

func TestSchemaConformance(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("schema-conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string
		Schema json.RawMessage
		Valid  bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(c.Schema)
			if (err == nil) != c.Valid {
				t.Fatalf("compile error=%v, want valid=%v", err, c.Valid)
			}
		})
	}
}

// Pin reviewed resources from jsonschema/v6 v6.0.2; see README.md.
func TestMetaschemaIntegrity(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("metaschemas.json")
	if err != nil {
		t.Fatal(err)
	}
	const want = "d0c095989a1f9441e5d2955303eae8a4dd54079cb252b99e4dc1dcd1e0efb009"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
		t.Fatalf("metaschemas.json hash=%s, want %s", got, want)
	}
}
