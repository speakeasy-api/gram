package mcpregistry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

var ErrUnsupportedUpdatedSince = errors.New("updated_since is unsupported by the discovery-only preview")

// DiscoveryOptions selects a live traversal, not a snapshot or synchronization feed.
type DiscoveryOptions struct {
	Search         string
	Version        string
	IncludeDeleted bool
	UpdatedSince   *string
	Cursor         string
	Limit          int32
}

type DiscoveryPage struct {
	Records    []json.RawMessage
	NextCursor string
}

type discoveryCursor struct {
	Name           string `json:"name"`
	Search         string `json:"search"`
	Version        string `json:"version"`
	IncludeDeleted bool   `json:"include_deleted"`
}

// Discover reads complete published records in one query. Retained installation
// Each page scans at most Limit candidates; invalid historical records can yield
// an empty page with a continuation cursor. Follow cursors until absent.
// Retained lookup deliberately remains separate, so local unpublishing cannot be bypassed.
func (s *Service) Discover(ctx context.Context, opts DiscoveryOptions) (DiscoveryPage, error) {
	if opts.UpdatedSince != nil {
		return DiscoveryPage{}, ErrUnsupportedUpdatedSince
	}
	if opts.Limit == 0 {
		opts.Limit = 25
	}
	if opts.Limit < 1 || opts.Limit > 100 || len(opts.Search) > 1024 || len(opts.Version) > 1024 || strings.ContainsRune(opts.Search+opts.Version, 0) {
		return DiscoveryPage{}, ErrInvalidListOptions
	}
	c := discoveryCursor{Name: "", Search: discoveryFilterBinding(opts.Search), Version: discoveryFilterBinding(opts.Version), IncludeDeleted: opts.IncludeDeleted}
	if opts.Cursor != "" {
		if len(opts.Cursor) > 8192 {
			return DiscoveryPage{}, ErrInvalidCursor
		}
		raw, err := base64.RawURLEncoding.DecodeString(opts.Cursor)
		if err != nil {
			return DiscoveryPage{}, ErrInvalidCursor
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		var decoded struct {
			Name           *string `json:"name"`
			Search         *string `json:"search"`
			Version        *string `json:"version"`
			IncludeDeleted *bool   `json:"include_deleted"`
		}
		if d.Decode(&decoded) != nil || decoded.Name == nil || decoded.Search == nil || decoded.Version == nil || decoded.IncludeDeleted == nil {
			return DiscoveryPage{}, ErrInvalidCursor
		}
		c = discoveryCursor{Name: *decoded.Name, Search: *decoded.Search, Version: *decoded.Version, IncludeDeleted: *decoded.IncludeDeleted}
		var extra any
		if d.Decode(&extra) != io.EOF || c.Name == "" || strings.ContainsRune(c.Name, 0) || c.Search != discoveryFilterBinding(opts.Search) || c.Version != discoveryFilterBinding(opts.Version) || c.IncludeDeleted != opts.IncludeDeleted {
			return DiscoveryPage{}, ErrInvalidCursor
		}
	}
	rows, err := repo.New(s.db).DiscoverEntries(ctx, repo.DiscoverEntriesParams{IncludeDeleted: opts.IncludeDeleted, Search: opts.Search, Version: opts.Version, AfterName: c.Name, PageLimit: opts.Limit + 1, ByteBudget: 16 << 20})
	if err != nil {
		return DiscoveryPage{}, fmt.Errorf("query discovery records: %w", err)
	}
	page := DiscoveryPage{Records: make([]json.RawMessage, 0, len(rows)), NextCursor: ""}
	responseBytes := 0
	for i, row := range rows {
		if row.DataBytes > 16<<20 && i == 0 {
			return DiscoveryPage{}, fmt.Errorf("discovery record exceeds page byte budget")
		}
		if i == int(opts.Limit) || row.PageBytes > 16<<20 {
			raw, err := json.Marshal(c)
			if err != nil {
				return DiscoveryPage{}, fmt.Errorf("encode discovery cursor: %w", err)
			}
			page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		if len(s.validator.ValidateStored(row.Data)) == 0 {
			data, err := discoveryMetadata(row.Data, row.IsLatest)
			if err != nil {
				return DiscoveryPage{}, err
			}
			if len(data) > 16<<20 {
				return DiscoveryPage{}, fmt.Errorf("discovery record exceeds page byte budget")
			}
			if responseBytes+len(data) > 16<<20 {
				raw, err := json.Marshal(c)
				if err != nil {
					return DiscoveryPage{}, fmt.Errorf("encode discovery cursor: %w", err)
				}
				page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
				break
			}
			responseBytes += len(data)
			page.Records = append(page.Records, data)
		}
		c.Name = row.DiscoveryName
	}
	return page, nil
}

// LookupDiscoveryVersion resolves the visible default or an exact published version.
func (s *Service) LookupDiscoveryVersion(ctx context.Context, name, version string, includeDeleted bool) (Entry, error) {
	if name == "" || version == "" || len(name) > 1024 || len(version) > 1024 || strings.ContainsRune(name+version, 0) {
		return Entry{}, ErrInvalidListOptions
	}
	result, err := repo.New(s.db).DiscoverVersion(ctx, repo.DiscoverVersionParams{Name: name, Version: version, IncludeDeleted: includeDeleted})
	row, err := entry(result.McpRegistryEntry, err)
	if err != nil {
		return Entry{}, err
	}
	if len(s.validator.ValidateStored(row.Data)) != 0 {
		return Entry{}, ErrNotFound
	}
	row.Data, err = discoveryMetadata(row.Data, result.IsLatest)
	return row, err
}

// Name is at most 200 ASCII bytes (enforced by SQL). Hex digests bind both
// filters without JSON escape expansion. The entire encoded cursor stays below
// 600 bytes even when accepted filters use their full 1024-byte allowance.
func discoveryFilterBinding(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

// Only backend-owned latest metadata changes; exact keys and numeric lexemes survive.
func discoveryMetadata(data json.RawMessage, latest bool) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("project discovery metadata: %w", err)
	}
	meta := map[string]json.RawMessage{}
	if raw := doc["_meta"]; raw != nil {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("project discovery metadata: %w", err)
		}
	}
	official := map[string]json.RawMessage{}
	const namespace = "io.modelcontextprotocol.registry/official"
	if raw := meta[namespace]; raw != nil {
		if err := json.Unmarshal(raw, &official); err != nil {
			return nil, fmt.Errorf("project discovery metadata: %w", err)
		}
	}
	official["isLatest"] = json.RawMessage("false")
	if latest {
		official["isLatest"] = json.RawMessage("true")
	}
	raw, err := encodeDiscoveryObject(official)
	if err != nil {
		return nil, fmt.Errorf("project discovery metadata: %w", err)
	}
	meta[namespace] = raw
	raw, err = encodeDiscoveryObject(meta)
	if err != nil {
		return nil, fmt.Errorf("project discovery metadata: %w", err)
	}
	doc["_meta"] = raw
	return encodeDiscoveryObject(doc)
}

func encodeDiscoveryObject(doc map[string]json.RawMessage) (json.RawMessage, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode discovery metadata: %w", err)
	}
	return bytes.TrimSpace(out.Bytes()), nil
}
