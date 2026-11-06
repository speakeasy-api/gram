package mcpregistry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

func refreshDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Compare content, not publication timestamps or destination-specific asset IDs.
func refreshSemantic(ctx context.Context, data json.RawMessage, read func(context.Context, string) ([]byte, error)) (json.RawMessage, error) {
	data, err := withoutPublicationMetadata(data)
	if err != nil {
		return nil, refreshError(err)
	}
	// Official lifecycle fields remain semantic; only timestamp noise is omitted.
	var root, meta, official map[string]json.RawMessage
	if err = json.Unmarshal(data, &root); err != nil {
		return nil, refreshError(err)
	}
	if raw := root["_meta"]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &meta); err != nil {
			return nil, refreshError(err)
		}
	}
	const officialNamespace = "io.modelcontextprotocol.registry/official"
	if raw := meta[officialNamespace]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &official); err != nil {
			return nil, refreshError(err)
		}
		delete(official, "publishedAt")
		delete(official, "updatedAt")
		delete(official, "statusChangedAt")
		if len(official) == 0 {
			delete(meta, officialNamespace)
		} else {
			meta[officialNamespace], _ = json.Marshal(official)
		}
		if len(meta) == 0 {
			delete(root, "_meta")
		} else {
			root["_meta"], _ = json.Marshal(meta)
		}
		data, err = json.Marshal(root)
		if err != nil {
			return nil, refreshError(err)
		}
	}
	icons, err := ParseCatalogIcons(data)
	if err != nil {
		return nil, refreshError(err)
	}
	for _, icon := range []*CatalogIcon{icons.Default, icons.Dark} {
		if icon == nil {
			continue
		}
		image, err := read(ctx, icon.Src)
		if err != nil {
			return nil, refreshError(err)
		}
		icon.Src = refreshDigest(image)
	}
	data, err = WithCatalogIcons(data, icons)
	if err != nil {
		return nil, refreshError(err)
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("normalize catalog record: %w", err)
	}
	raw, err := json.Marshal(value)
	return raw, refreshError(err)
}

func refreshDecision(base, local, upstream json.RawMessage, published bool) (string, string) {
	if len(base) > 0 && equalJSON(base, upstream) {
		return "preserved", "upstream_unchanged"
	}
	if len(upstream) == 0 {
		if len(base) == 0 || len(local) == 0 {
			return "preserved", "destination_only"
		}
		if !published && equalJSON(base, local) {
			return "preserved", "publication_changed"
		}
		if equalJSON(base, local) {
			return "unpublish", "upstream_removed"
		}
		return "conflict", "locally_modified"
	}
	if len(local) == 0 {
		if len(base) == 0 {
			return "create", ""
		}
		return "conflict", "destination_removed"
	}
	if !published {
		return "conflict", "publication_changed"
	}
	if checkStructure(local, upstream) != nil {
		return "conflict", "immutable_structure"
	}
	if equalJSON(local, upstream) {
		return "adopt", ""
	}
	if len(base) == 0 {
		return "conflict", "missing_baseline"
	}
	if !equalJSON(base, local) {
		return "conflict", "locally_modified"
	}
	return "update", ""
}

func refreshNames(maps ...map[string]json.RawMessage) []string {
	set := map[string]bool{}
	for _, m := range maps {
		for name := range m {
			set[name] = true
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
