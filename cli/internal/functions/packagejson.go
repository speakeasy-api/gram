package functions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// jsonObject is a JSON object that keeps its key order, so rewriting a
// template's package.json does not shuffle it.
type jsonObject []jsonField

type jsonField struct {
	Key   string
	Value json.RawMessage
}

func (o *jsonObject) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return errors.New("expected a JSON object")
	}

	var fields jsonObject
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("expected an object key")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("read %q: %w", key, err)
		}
		fields = append(fields, jsonField{Key: key, Value: value})
	}
	*o = fields
	return nil
}

func (o jsonObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshalNoEscape(f.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(f.Value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o jsonObject) get(key string) (json.RawMessage, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// set replaces key's value in place, or appends key when it is missing.
func (o *jsonObject) set(key string, value any) error {
	raw, err := marshalNoEscape(value)
	if err != nil {
		return err
	}
	for i, f := range *o {
		if f.Key == key {
			(*o)[i].Value = raw
			return nil
		}
	}
	*o = append(*o, jsonField{Key: key, Value: raw})
	return nil
}

// object decodes key's value as a JSON object. A missing key gives nil.
func (o jsonObject) object(key string) (jsonObject, error) {
	raw, ok := o.get(key)
	if !ok {
		return nil, nil
	}
	var obj jsonObject
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return obj, nil
}

// marshalNoEscape encodes v without escaping <, > and &, which package.json
// scripts use.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func readPackageJSON(path string) (jsonObject, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read package.json: %w", err)
	}
	var pkg jsonObject
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return pkg, nil
}

func writePackageJSON(path string, pkg jsonObject) error {
	compact, err := pkg.MarshalJSON()
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return fmt.Errorf("format package.json: %w", err)
	}
	out.WriteByte('\n')
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}
	return nil
}

// packageJSONRewrite holds the values a scaffolded package.json takes.
type packageJSONRewrite struct {
	Name string
	// Dependencies replaces the version of each listed dependency that the
	// template already declares.
	Dependencies map[string]string
}

// rewritePackageJSON turns a template's package.json into the new project's:
// it sets the name, resets the version, pins the template's placeholder
// dependency versions and drops the "_:" prefix from scripts the template
// hides from the monorepo.
func rewritePackageJSON(path string, rw packageJSONRewrite) error {
	pkg, err := readPackageJSON(path)
	if err != nil {
		return err
	}

	if err := pkg.set("name", rw.Name); err != nil {
		return err
	}
	if err := pkg.set("version", "0.0.0"); err != nil {
		return err
	}

	deps, err := pkg.object("dependencies")
	if err != nil {
		return err
	}
	if deps != nil {
		for name, version := range rw.Dependencies {
			if _, ok := deps.get(name); !ok {
				continue
			}
			if err := deps.set(name, version); err != nil {
				return err
			}
		}
		if err := pkg.set("dependencies", deps); err != nil {
			return err
		}
	}

	scripts, err := pkg.object("scripts")
	if err != nil {
		return err
	}
	if scripts != nil {
		for i, f := range scripts {
			scripts[i].Key = strings.TrimPrefix(f.Key, "_:")
		}
		if err := pkg.set("scripts", scripts); err != nil {
			return err
		}
	}

	return writePackageJSON(path, pkg)
}
