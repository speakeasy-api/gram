package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

const IconsNamespace = "com.speakeasy.ai/icons"

type CatalogIcon struct {
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType"`
	Sizes    []string `json:"sizes"`
}
type CatalogIcons struct {
	Default *CatalogIcon `json:"default,omitempty"`
	Dark    *CatalogIcon `json:"dark,omitempty"`
}

func ParseCatalogIcons(data json.RawMessage) (CatalogIcons, error) {
	var root struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	var icons CatalogIcons
	if err := json.Unmarshal(data, &root); err != nil {
		return icons, err
	}
	if raw, ok := root.Meta[IconsNamespace]; ok {
		if err := json.Unmarshal(raw, &icons); err != nil {
			return icons, err
		}
	}
	return icons, nil
}

// WithCatalogIcons preserves unrelated JSON values without decoding numbers.
func WithCatalogIcons(data json.RawMessage, icons CatalogIcons) (json.RawMessage, error) {
	var root, meta map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("registry record must be an object")
	}
	if raw, ok := root["_meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, err
		}
	}
	if icons.Default == nil && icons.Dark == nil {
		delete(meta, IconsNamespace)
	} else {
		if meta == nil {
			meta = make(map[string]json.RawMessage)
		}
		raw, err := json.Marshal(icons)
		if err != nil {
			return nil, err
		}
		meta[IconsNamespace] = raw
	}
	if len(meta) == 0 {
		delete(root, "_meta")
	} else {
		raw, err := json.Marshal(meta)
		if err != nil {
			return nil, err
		}
		root["_meta"] = raw
	}
	return json.Marshal(root)
}

// WithAssetBaseURL copies startup configuration; do not call per request.
func (s *Service) WithAssetBaseURL(base *url.URL) *Service {
	s.assetBaseURL = nil
	if base != nil {
		copied := *base
		s.assetBaseURL = &copied
	}
	return s
}

func (s *Service) canonicalIcons(ctx context.Context, q *repo.Queries, data json.RawMessage) (json.RawMessage, error) {
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if _, ok := envelope.Meta[IconsNamespace]; !ok {
		return data, nil
	}
	icons, err := ParseCatalogIcons(data)
	if err != nil {
		return nil, err
	}
	for _, slot := range []struct {
		name string
		icon *CatalogIcon
	}{{"default", icons.Default}, {"dark", icons.Dark}} {
		if slot.icon == nil {
			continue
		}
		invalid := &InvalidError{Issues: []Issue{{Path: "/_meta/com.speakeasy.ai~1icons/" + slot.name + "/src", Message: "must reference a public platform SVG asset"}}}
		id, err := s.catalogIconAssetID(slot.icon.Src)
		if err != nil {
			return nil, invalid
		}
		valid, err := q.IsCatalogSVGAsset(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("validate catalog icon asset: %w", err)
		}
		if !valid {
			return nil, invalid
		}
	}
	return WithCatalogIcons(data, icons)
}

func (s *Service) catalogIconAssetID(src string) (uuid.UUID, error) {
	base := s.assetBaseURL
	if base == nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil {
		return uuid.Nil, fmt.Errorf("missing valid asset base URL")
	}
	u, err := url.Parse(src)
	if err != nil {
		return uuid.Nil, err
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || u.User != nil || u.Fragment != "" || len(query) != 1 || len(query["id"]) != 1 {
		return uuid.Nil, fmt.Errorf("invalid asset URL")
	}
	id, err := uuid.Parse(query.Get("id"))
	if err != nil || id == uuid.Nil || src != assets.ServeImageURL(base, id) {
		return uuid.Nil, fmt.Errorf("noncanonical asset URL")
	}
	return id, nil
}

// IsPublicCatalogIcon checks stored metadata against trusted startup configuration
// and the current asset association. Legacy rows have not necessarily passed write validation.
func (s *Service) IsPublicCatalogIcon(ctx context.Context, icon *CatalogIcon) bool {
	if s == nil || icon == nil || len(icon.Src) > 2048 || icon.MimeType != "image/svg+xml" || len(icon.Sizes) != 1 || icon.Sizes[0] != "any" {
		return false
	}
	id, err := s.catalogIconAssetID(icon.Src)
	if err != nil || s.db == nil {
		return false
	}
	valid, err := repo.New(s.db).IsCatalogSVGAsset(ctx, id)
	return err == nil && valid
}
