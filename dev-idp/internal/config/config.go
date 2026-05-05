// Package config parses dev-idp's runtime configuration. The single
// database knob, GRAM_DEVIDP_DB, accepts:
//
//	memory          - in-memory SQLite (per-connection scope; tests pin
//	                  MaxOpenConns=1 so all queries share one connection).
//	file:<path>     - file-backed SQLite at the given path. Relative paths
//	                  resolve from the working directory.
//	(unset)         - defaults to file:local/devidp/devidp.db.
package config

import (
	"fmt"
	"strings"
)

const (
	DefaultDBSpec = "file:local/devidp/devidp.db"
	envDBKey      = "GRAM_DEVIDP_DB"
)

type DBMode int

const (
	DBModeFile DBMode = iota
	DBModeMemory
)

type DB struct {
	Mode DBMode
	Path string
}

func ParseDB(spec string) (DB, error) {
	if spec == "" {
		spec = DefaultDBSpec
	}
	if spec == "memory" || spec == ":memory:" {
		return DB{Mode: DBModeMemory, Path: ""}, nil
	}
	rest, ok := strings.CutPrefix(spec, "file:")
	if !ok {
		return DB{}, fmt.Errorf("unrecognized %s value %q (expected 'memory' or 'file:<path>')", envDBKey, spec)
	}
	if rest == "" {
		return DB{}, fmt.Errorf("%s=file: requires a path", envDBKey)
	}
	return DB{Mode: DBModeFile, Path: rest}, nil
}
