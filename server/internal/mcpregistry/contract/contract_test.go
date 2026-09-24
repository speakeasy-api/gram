package contract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("contract-cases.json")
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
func TestStarterBaseline(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../baseline/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Count   int `json:"count"`
		Records []struct {
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

func TestNumberPrecision(t *testing.T) {
	value, err := decode([]byte(`{"server":{"name":"io.example/fixture","description":"Fixture","version":"1"},"extension":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := Compile(Schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
	if got := value.(map[string]any)["extension"]; got != json.Number("9007199254740993") {
		t.Fatalf("number changed: %v", got)
	}
}
