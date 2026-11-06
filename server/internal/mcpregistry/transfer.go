package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

const CatalogBundleVersion = 1
const CatalogBundleByteLimit = 64 << 20

// CatalogBundle is an explicit, offline, published-only transfer format. Assets
// are keyed by the source public URL, but import never fetches those URLs.
// It contains catalog records only, never installations or tenant configuration.
type CatalogBundle struct {
	Version    int               `json:"version"`
	ExportedAt time.Time         `json:"exportedAt"`
	Entries    []json.RawMessage `json:"entries"`
	Assets     map[string][]byte `json:"assets"`
}

type CatalogAssetReader interface {
	ReadCatalogAsset(context.Context, string) ([]byte, error)
}
type CatalogAssetWriter interface {
	StoreCatalogAsset(context.Context, []byte) (string, error)
}

type CatalogImportResult struct {
	Created   []string `json:"created"`
	Conflicts []string `json:"conflicts"`
}

// CatalogImporter is configured by the trusted deployment composition root,
// never by an HTTP request. Only local and dev are writable destinations.
type CatalogImporter struct {
	registry    *Service
	environment string
	assets      CatalogAssetWriter
}

func NewCatalogImporter(s *Service, environment string, images CatalogAssetWriter) *CatalogImporter {
	return &CatalogImporter{registry: s, environment: environment, assets: images}
}

func DecodeCatalogBundle(r io.Reader) (CatalogBundle, error) {
	var b CatalogBundle
	raw, err := io.ReadAll(io.LimitReader(r, CatalogBundleByteLimit+1))
	if err != nil {
		return b, fmt.Errorf("decode catalog bundle: %w", err)
	}
	if len(raw) > CatalogBundleByteLimit {
		return b, fmt.Errorf("catalog bundle exceeds size limit")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&b); err != nil {
		return b, fmt.Errorf("decode catalog bundle: %w", err)
	}
	if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return b, fmt.Errorf("trailing catalog bundle data")
	}
	return b, nil
}

// ExportCatalog uses a consistent read-only database snapshot, not discovery's
// filtered/paginated projection. Invalid records or missing assets fail the whole export.
func (s *Service) ExportCatalog(ctx context.Context, images CatalogAssetReader) (CatalogBundle, error) {
	b := CatalogBundle{Version: CatalogBundleVersion, Entries: []json.RawMessage{}, ExportedAt: time.Now().UTC(), Assets: map[string][]byte{}}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return b, fmt.Errorf("export catalog: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	size, err := repo.New(tx).PublishedCatalogSize(ctx)
	if err != nil {
		return b, fmt.Errorf("export catalog: %w", err)
	}
	if size.Entries > 10000 || size.Bytes > CatalogBundleByteLimit {
		return b, fmt.Errorf("catalog exceeds bundle limits")
	}
	records, err := repo.New(tx).ExportPublishedCatalog(ctx)
	if err != nil {
		return b, fmt.Errorf("export catalog: %w", err)
	}
	for _, record := range records {
		b.Entries = append(b.Entries, json.RawMessage(record))
	}
	for _, data := range b.Entries {
		icons, err := ParseCatalogIcons(data)
		if err != nil {
			return b, fmt.Errorf("export catalog: %w", err)
		}
		for _, icon := range []*CatalogIcon{icons.Default, icons.Dark} {
			if icon == nil {
				continue
			}
			if _, ok := b.Assets[icon.Src]; ok {
				continue
			}
			id, err := s.catalogIconAssetID(icon.Src)
			if err != nil {
				return b, fmt.Errorf("export catalog: %w", err)
			}
			valid, err := repo.New(tx).IsCatalogSVGAsset(ctx, id)
			if err != nil {
				return b, fmt.Errorf("export catalog: %w", err)
			}
			if !valid {
				return b, fmt.Errorf("catalog icon is not a public platform SVG")
			}
			if images == nil {
				return b, fmt.Errorf("catalog asset reader required")
			}
			raw, err := images.ReadCatalogAsset(ctx, icon.Src)
			if err != nil {
				return b, fmt.Errorf("export catalog: %w", err)
			}
			b.Assets[icon.Src] = raw
			size.Bytes += int64(len(raw)) * 2
			if size.Bytes > CatalogBundleByteLimit {
				return b, fmt.Errorf("catalog exceeds bundle limits")
			}
		}
	}
	if err := s.validateCatalogBundle(ctx, b, assets.ValidateCatalogSVG); err != nil {
		return b, fmt.Errorf("export catalog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return b, fmt.Errorf("commit catalog export: %w", err)
	}
	return b, nil
}

func (s *Service) validateCatalogBundle(ctx context.Context, b CatalogBundle, validateSVG func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate catalog bundle canceled: %w", err)
	}
	if b.Version != CatalogBundleVersion {
		return fmt.Errorf("unsupported catalog bundle version")
	}
	if len(b.Entries) > 10000 {
		return fmt.Errorf("catalog entry limit exceeded")
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("validate catalog bundle: %w", err)
	}
	if len(raw) > CatalogBundleByteLimit {
		return fmt.Errorf("catalog bundle exceeds size limit")
	}
	names := map[string]bool{}
	used := map[string]bool{}
	for _, data := range b.Entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate catalog records canceled: %w", err)
		}
		if issues := s.validator.Validate(data); len(issues) > 0 {
			return &InvalidError{Issues: issues}
		}
		name := recordName(data)
		if names[name] {
			return fmt.Errorf("duplicate catalog name")
		}
		names[name] = true
		icons, err := ParseCatalogIcons(data)
		if err != nil {
			return fmt.Errorf("validate catalog bundle: %w", err)
		}
		for _, icon := range []*CatalogIcon{icons.Default, icons.Dark} {
			if icon == nil {
				continue
			}
			if icon.MimeType != "image/svg+xml" || len(icon.Sizes) != 1 || icon.Sizes[0] != "any" {
				return fmt.Errorf("invalid catalog icon metadata")
			}
			_, ok := b.Assets[icon.Src]
			if !ok {
				return fmt.Errorf("missing catalog logo bytes")
			}
			used[icon.Src] = true
		}
	}
	if len(used) != len(b.Assets) {
		return fmt.Errorf("unreferenced catalog assets")
	}
	// Validate unique assets, not references: thousands of entries may share one logo.
	for _, image := range b.Assets {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("validate catalog assets canceled: %w", err)
		}
		if err := validateSVG(image); err != nil {
			return fmt.Errorf("invalid catalog logo: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate catalog assets canceled: %w", err)
	}
	return nil
}

// Import validates the entire bundle before storing assets, and publishes all
// new entries in one transaction. Existing names are never overwritten, even
// when unpublished or locally edited. No baseline/reset semantics are implied.
func (i *CatalogImporter) Import(ctx context.Context, b CatalogBundle) (CatalogImportResult, error) {
	result := CatalogImportResult{Created: []string{}, Conflicts: []string{}}
	if i.environment != "local" && i.environment != "dev" {
		return result, fmt.Errorf("catalog import destination must be local or dev")
	}
	s := i.registry
	if err := s.validateCatalogBundle(ctx, b, assets.ValidateCatalogSVG); err != nil {
		return result, fmt.Errorf("validate import bundle: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin catalog import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize against all catalog edits/creates, including non-import writers.
	// Asset uploads use separate transactions and never mutate registry entries.
	if err = repo.New(tx).LockCatalogTransfer(ctx); err != nil {
		return result, fmt.Errorf("lock catalog for import: %w", err)
	}
	rewritten := map[string]string{}
	for _, data := range b.Entries {
		taken, err := NameTakenTx(ctx, tx, data)
		if err != nil {
			return result, fmt.Errorf("check destination name conflict: %w", err)
		}
		name := recordName(data)
		if taken {
			result.Conflicts = append(result.Conflicts, name)
			continue
		}
		icons, err := ParseCatalogIcons(data)
		if err != nil {
			return result, fmt.Errorf("parse imported catalog icons: %w", err)
		}
		for _, icon := range []*CatalogIcon{icons.Default, icons.Dark} {
			if icon == nil {
				continue
			}
			dst, ok := rewritten[icon.Src]
			if !ok {
				if i.assets == nil {
					return result, fmt.Errorf("catalog asset writer required")
				}
				dst, err = i.assets.StoreCatalogAsset(ctx, b.Assets[icon.Src])
				if err != nil {
					return result, fmt.Errorf("store destination catalog logo: %w", err)
				}
				rewritten[icon.Src] = dst
			}
			icon.Src = dst
		}
		data, err = WithCatalogIcons(data, icons)
		if err != nil {
			return result, fmt.Errorf("rewrite destination catalog icon references: %w", err)
		}
		data, err = withoutPublicationMetadata(data)
		if err != nil {
			return result, fmt.Errorf("rebase catalog publication metadata: %w", err)
		}
		if _, err = s.CreateTx(ctx, tx, data); err != nil {
			return CatalogImportResult{}, fmt.Errorf("create imported catalog entry: %w", err)
		}
		result.Created = append(result.Created, name)
	}
	if err = tx.Commit(ctx); err != nil {
		return CatalogImportResult{}, fmt.Errorf("commit catalog import transaction: %w", err)
	}
	return result, nil
}

func withoutPublicationMetadata(data json.RawMessage) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("without publication metadata: %w", err)
	}
	if raw, ok := root["_meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("without publication metadata: %w", err)
		}
	}
	delete(meta, publicationNamespace)
	if len(meta) == 0 {
		delete(root, "_meta")
	} else {
		raw, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("without publication metadata: %w", err)
		}
		root["_meta"] = raw
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode catalog metadata: %w", err)
	}
	return raw, nil
}
