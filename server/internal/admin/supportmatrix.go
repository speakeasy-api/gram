package admin

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

//go:embed supportmatrix/catalog.json
var supportCatalog []byte

// Account types and eligibility values are closed sets shared by the catalog,
// the API and the admin UI. The transport layer rejects unknown values, so
// these guard the service when it is called directly.
var supportAccountTypes = []string{"personal", "team", "enterprise"}

func validSupportEligibility(value string) bool {
	return value == "supported" || value == "unsupported" || value == "unknown"
}

func validSupportAccountType(value string) bool {
	return slices.Contains(supportAccountTypes, value)
}

// SeedSupportMatrix inserts missing global catalog entries without replacing
// operator edits or inferring platform applicability from reference claims.
// Presentation columns do follow the catalog, because nothing else can correct
// them once a row exists.
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
		if err := liftLabelledAccountConditions(ctx, q); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed support matrix transaction: %w", err)
	}
	return nil
}

// Claims the UI used to append to a mapping's free-text conditions, one per
// account type, before account eligibility had a column of its own.
var labelledAccountClaim = regexp.MustCompile(`(?i)^(personal accounts?|team plans?|enterprise(?: accounts?| plans?)?)\s*:\s*(.*)$`)

var (
	ineligibleClaim = regexp.MustCompile(`(?i)☠|❌|×|not possible|not supported|unsupported|^no$|^false$`)
	eligibleClaim   = regexp.MustCompile(`(?i)✅|✓|^supported$|^yes$|^true$`)
)

// splitLabelledAccountConditions separates a mapping's conditions into
// structured eligibility and the free text that remains. Claims it cannot read
// as an answer become "unknown" rather than silently dropping out, so an
// unreadable claim is visible in the UI instead of looking like an assessment
// nobody made.
func splitLabelledAccountConditions(conditions string) (map[string]string, string) {
	accounts := map[string]string{}
	remaining := make([]string, 0, 4)
	for _, part := range strings.FieldsFunc(conditions, func(r rune) bool { return r == ';' || r == '\n' }) {
		claim := labelledAccountClaim.FindStringSubmatch(strings.TrimSpace(part))
		if claim == nil {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				remaining = append(remaining, trimmed)
			}
			continue
		}
		accountType := "enterprise"
		switch strings.ToLower(claim[1][0:1]) {
		case "p":
			accountType = "personal"
		case "t":
			accountType = "team"
		}
		value := strings.TrimSpace(claim[2])
		switch {
		case strings.Contains(strings.ToLower(value), "enterprise only"):
			accounts[accountType] = "unsupported"
			if accountType == "enterprise" {
				accounts[accountType] = "supported"
			}
		case ineligibleClaim.MatchString(value):
			accounts[accountType] = "unsupported"
		case eligibleClaim.MatchString(value):
			accounts[accountType] = "supported"
		default:
			accounts[accountType] = "unknown"
		}
	}
	return accounts, strings.Join(remaining, "; ")
}

func liftLabelledAccountConditions(ctx context.Context, q *repo.Queries) error {
	rows, err := q.ListSupportMappingsWithLabelledConditions(ctx)
	if err != nil {
		return fmt.Errorf("read labelled support conditions: %w", err)
	}
	for _, row := range rows {
		accounts, conditions := splitLabelledAccountConditions(row.Conditions)
		if len(accounts) == 0 {
			continue
		}
		raw, err := marshalSupportAccounts(accounts)
		if err != nil {
			return err
		}
		if err := q.SetSupportMappingAccounts(ctx, repo.SetSupportMappingAccountsParams{Accounts: raw, Conditions: conditions, ID: row.ID}); err != nil {
			return fmt.Errorf("lift labelled support conditions: %w", err)
		}
	}
	return nil
}

// marshalSupportAccounts keeps a nil map out of the column: the eligibility
// columns are NOT NULL and readers expect an object, not JSON null.
func marshalSupportAccounts(accounts map[string]string) ([]byte, error) {
	if accounts == nil {
		accounts = map[string]string{}
	}
	raw, err := json.Marshal(accounts)
	if err != nil {
		return nil, fmt.Errorf("encode account eligibility: %w", err)
	}
	return raw, nil
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
	if draft == nil || draft.Mappings == nil || draft.References == nil || draft.Accounts == nil {
		return oops.E(oops.CodeInvalid, nil, "mappings, references and accounts are required")
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
	validateAccounts := func(accounts map[string]string) error {
		for accountType, eligibility := range accounts {
			if !validSupportAccountType(accountType) || !validSupportEligibility(eligibility) {
				return oops.E(oops.CodeInvalid, nil, "unknown account type or account eligibility")
			}
		}
		return nil
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
		if strings.ContainsRune(fact.Note, 0) {
			return oops.E(oops.CodeInvalid, nil, "notes must not contain NUL characters")
		}
		if utf8.RuneCountInString(fact.Note) > 10000 || (fact.Status == "partial" && strings.TrimSpace(fact.Note) == "") {
			return oops.E(oops.CodeInvalid, nil, "partial coverage requires notes; notes must be at most 10000 characters")
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
		if strings.ContainsRune(mapping.Conditions, 0) {
			return oops.E(oops.CodeInvalid, nil, "conditions must not contain NUL characters")
		}
		if utf8.RuneCountInString(mapping.Conditions) > 10000 {
			return oops.E(oops.CodeInvalid, nil, "conditions must be at most 10000 characters")
		}
		if err := validateAccounts(mapping.Accounts); err != nil {
			return err
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
	for method, accounts := range draft.Accounts {
		if !methodIDs[method] || accounts == nil {
			return oops.E(oops.CodeInvalid, nil, "unknown integration method or missing account eligibility")
		}
		if err := validateAccounts(accounts); err != nil {
			return err
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
			accounts, err := marshalSupportAccounts(mapping.Accounts)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "save support mapping")
			}
			id, err := q.UpsertSupportMapping(ctx, repo.UpsertSupportMappingParams{MethodSlug: method, PlatformSlug: platform, Applicability: mapping.Applicability, Conditions: mapping.Conditions, Accounts: accounts})
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
		for method, eligibility := range payload.Draft.Accounts {
			if reflect.DeepEqual(eligibility, current.Draft.Accounts[method]) {
				continue
			}
			accounts, err := marshalSupportAccounts(eligibility)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "save method account eligibility")
			}
			if err := q.UpsertSupportMethodAccounts(ctx, repo.UpsertSupportMethodAccountsParams{MethodSlug: method, Accounts: accounts}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "save method account eligibility")
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
