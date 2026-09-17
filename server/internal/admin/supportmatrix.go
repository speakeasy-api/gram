package admin

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

//go:embed supportmatrix/catalog.json
var supportCatalog []byte

// SeedSupportMatrix inserts missing global catalog entries without replacing
// operator edits or inferring platform applicability from reference claims.
func SeedSupportMatrix(ctx context.Context, db *pgxpool.Pool) error {
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockSupportMatrix(ctx); err != nil {
			return fmt.Errorf("lock support catalog: %w", err)
		}
		for _, seed := range []func(context.Context, []byte) error{q.SeedSupportPlatforms, q.SeedSupportMethods, q.SeedSupportCapabilities, q.SeedSupportReferences} {
			if err := seed(ctx, supportCatalog); err != nil {
				return fmt.Errorf("seed support catalog: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed support matrix transaction: %w", err)
	}
	return nil
}

func readSupportMatrix(ctx context.Context, q *repo.Queries) (*gen.SupportMatrix, error) {
	raw, err := q.ReadSupportMatrix(ctx)
	if err != nil {
		return nil, fmt.Errorf("read support matrix: %w", err)
	}
	var result gen.SupportMatrix
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode support matrix: %w", err)
	}
	hash := sha256.Sum256(raw)
	result.Revision = hex.EncodeToString(hash[:])
	return &result, nil
}

func (s *Service) GetSupportMatrix(ctx context.Context, _ *gen.GetSupportMatrixPayload) (*gen.SupportMatrix, error) {
	result, err := readSupportMatrix(ctx, repo.New(s.db))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read support matrix").LogError(ctx, s.logger)
	}
	return result, nil
}

func validateSupportDraft(draft *gen.SupportDraft, catalog *gen.SupportMatrix) error {
	if draft == nil || draft.Mappings == nil || draft.References == nil {
		return oops.E(oops.CodeInvalid, nil, "mappings and references are required")
	}
	methodIDs := make(map[string]bool, len(catalog.Methods))
	productIDs := make(map[string]bool, len(catalog.Products))
	capabilityIDs := make(map[string]bool, len(catalog.Capabilities))
	for _, method := range catalog.Methods {
		methodIDs[method.ID] = true
	}
	for _, product := range catalog.Products {
		productIDs[product.ID] = true
	}
	for _, capability := range catalog.Capabilities {
		capabilityIDs[capability.ID] = true
	}
	validateFact := func(id string, fact *gen.SupportFact) error {
		if !capabilityIDs[id] || fact == nil {
			return oops.E(oops.CodeInvalid, nil, "unknown capability or missing coverage fact")
		}
		switch fact.Status {
		case "supported", "partial", "unimplemented", "impossible", "na", "unknown":
		default:
			return oops.E(oops.CodeInvalid, nil, "invalid coverage status")
		}
		if len(fact.Note) > 10000 || (fact.Status == "partial" && strings.TrimSpace(fact.Note) == "") {
			return oops.E(oops.CodeInvalid, nil, "partial coverage requires notes; notes must be at most 10000 bytes")
		}
		return nil
	}
	for key, mapping := range draft.Mappings {
		method, product, ok := strings.Cut(key, "/")
		if !ok || !methodIDs[method] || !productIDs[product] || mapping == nil || mapping.Facts == nil {
			return oops.E(oops.CodeInvalid, nil, "unknown method/platform mapping or missing facts")
		}
		if mapping.Applicability != "unknown" && mapping.Applicability != "applicable" && mapping.Applicability != "na" {
			return oops.E(oops.CodeInvalid, nil, "invalid applicability")
		}
		if len(mapping.Conditions) > 10000 {
			return oops.E(oops.CodeInvalid, nil, "conditions must be at most 10000 bytes")
		}
		for id, fact := range mapping.Facts {
			if err := validateFact(id, fact); err != nil {
				return err
			}
		}
	}
	for method, facts := range draft.References {
		if !methodIDs[method] || facts == nil {
			return oops.E(oops.CodeInvalid, nil, "unknown integration method or missing reference facts")
		}
		for id, fact := range facts {
			if err := validateFact(id, fact); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) UpdateSupportMatrix(ctx context.Context, payload *gen.UpdateSupportMatrixPayload) (*gen.SupportMatrix, error) {
	var result *gen.SupportMatrix
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockSupportMatrix(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock support matrix")
		}
		current, err := readSupportMatrix(ctx, q)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read support matrix")
		}
		if payload.Revision != current.Revision {
			return oops.E(oops.CodeConflict, nil, "Support matrix changed. Reload the matrix before saving again.")
		}
		if err := validateSupportDraft(payload.Draft, current); err != nil {
			return err
		}
		for key, mapping := range payload.Draft.Mappings {
			previous := current.Draft.Mappings[key]
			if reflect.DeepEqual(mapping, previous) {
				continue
			}
			method, platform, _ := strings.Cut(key, "/")
			id, err := q.UpsertSupportMapping(ctx, repo.UpsertSupportMappingParams{MethodSlug: method, PlatformSlug: platform, Applicability: mapping.Applicability, Conditions: mapping.Conditions})
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "save support mapping")
			}
			for capability, fact := range mapping.Facts {
				if previous != nil && reflect.DeepEqual(fact, previous.Facts[capability]) {
					continue
				}
				err = q.UpsertSupportCoverage(ctx, repo.UpsertSupportCoverageParams{MappingID: id, CapabilitySlug: capability, Status: fact.Status, Notes: fact.Note, NeedsVerification: fact.Verify})
				if err != nil {
					return oops.E(oops.CodeUnexpected, err, "save support coverage")
				}
			}
		}
		for method, facts := range payload.Draft.References {
			for capability, fact := range facts {
				if reflect.DeepEqual(fact, current.Draft.References[method][capability]) {
					continue
				}
				err = q.UpsertSupportReference(ctx, repo.UpsertSupportReferenceParams{MethodSlug: method, CapabilitySlug: capability, Status: fact.Status, Notes: fact.Note, NeedsVerification: fact.Verify})
				if err != nil {
					return oops.E(oops.CodeUnexpected, err, "save support reference")
				}
			}
		}
		result, err = readSupportMatrix(ctx, q)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read saved support matrix")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save support matrix transaction: %w", err)
	}
	return result, nil
}
