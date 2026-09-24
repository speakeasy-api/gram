package contract

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRecordContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/contract-cases.json")
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
func TestNumberPrecision(t *testing.T) {
	t.Parallel()
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
	record, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("record is %T, want object", value)
	}
	if got := record["extension"]; got != json.Number("9007199254740993") {
		t.Fatalf("number changed: %v", got)
	}
}
