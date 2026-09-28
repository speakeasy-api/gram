package organizations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	admingen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	onboardingNameMaxLength        = 200
	onboardingDescriptionMaxLength = 2000
	onboardingSlugMaxLength        = 64
	// uniqueViolation is the Postgres code a duplicate use case slug raises.
	uniqueViolation = "23505"
)

var onboardingSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// OnboardingPlaybookInput is what staff give to create or replace a playbook.
type OnboardingPlaybookInput struct {
	// UseCaseID makes the playbook shared: the use case it belongs to.
	UseCaseID *uuid.UUID
	// OrganizationID makes the playbook custom to one organization. Exactly
	// one of UseCaseID and OrganizationID is set.
	OrganizationID *string
	Name           string
	Description    string
	// IsDefault marks the use case's default; only a playbook without an
	// organization can be one.
	IsDefault bool
	// StepSlugs are top-level steps in walking order.
	StepSlugs []string
}

func useCaseView(row repo.GetOnboardingUseCaseRow, defaultPlaybook uuid.NullUUID) *admingen.AdminOnboardingUseCase {
	view := &admingen.AdminOnboardingUseCase{ID: row.ID.String(), Slug: row.Slug, Name: row.Name, Description: row.Description, DefaultPlaybookID: nil}
	if defaultPlaybook.Valid {
		view.DefaultPlaybookID = conv.PtrEmpty(defaultPlaybook.UUID.String())
	}
	return view
}

// ListOnboardingUseCases reads the use cases staff defined, each with its
// default playbook.
func ListOnboardingUseCases(ctx context.Context, db repo.DBTX) (*admingen.AdminOnboardingUseCaseList, error) {
	rows, err := repo.New(db).ListOnboardingUseCases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list onboarding use cases: %w", err)
	}
	list := &admingen.AdminOnboardingUseCaseList{UseCases: make([]*admingen.AdminOnboardingUseCase, 0, len(rows))}
	for _, row := range rows {
		list.UseCases = append(list.UseCases, useCaseView(repo.GetOnboardingUseCaseRow{ID: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description, SortOrder: row.SortOrder}, row.DefaultPlaybookID))
	}
	return list, nil
}

func validateOnboardingText(name, description string) error {
	if strings.TrimSpace(name) == "" {
		return oops.E(oops.CodeBadRequest, nil, "name is required")
	}
	if utf8.RuneCountInString(name) > onboardingNameMaxLength {
		return oops.E(oops.CodeBadRequest, nil, "name must be at most %d characters", onboardingNameMaxLength)
	}
	if utf8.RuneCountInString(description) > onboardingDescriptionMaxLength {
		return oops.E(oops.CodeBadRequest, nil, "description must be at most %d characters", onboardingDescriptionMaxLength)
	}
	return nil
}

// CreateOnboardingUseCase defines a use case. Its caller authenticates staff.
func CreateOnboardingUseCase(ctx context.Context, db repo.DBTX, slug, name, description string) (*admingen.AdminOnboardingUseCase, error) {
	slug = strings.TrimSpace(slug)
	if !onboardingSlugPattern.MatchString(slug) || len(slug) > onboardingSlugMaxLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug must be lowercase letters, digits and dashes, at most %d characters", onboardingSlugMaxLength)
	}
	if err := validateOnboardingText(name, description); err != nil {
		return nil, err
	}
	row, err := repo.New(db).CreateOnboardingUseCase(ctx, repo.CreateOnboardingUseCaseParams{Slug: slug, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description)})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "a use case with slug %q already exists", slug)
		}
		return nil, fmt.Errorf("create onboarding use case: %w", err)
	}
	return useCaseView(repo.GetOnboardingUseCaseRow(row), uuid.NullUUID{UUID: uuid.Nil, Valid: false}), nil
}

// UpdateOnboardingUseCase renames or describes a use case.
func UpdateOnboardingUseCase(ctx context.Context, db repo.DBTX, id uuid.UUID, name, description string) (*admingen.AdminOnboardingUseCase, error) {
	if err := validateOnboardingText(name, description); err != nil {
		return nil, err
	}
	queries := repo.New(db)
	row, err := queries.UpdateOnboardingUseCase(ctx, repo.UpdateOnboardingUseCaseParams{ID: id, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("update onboarding use case: %w", err)
	}
	def, err := queries.GetOnboardingDefaultPlaybook(ctx, conv.ToNullUUID(id))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load default playbook: %w", err)
	}
	defaultID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if err == nil {
		defaultID = uuid.NullUUID{UUID: def.ID, Valid: true}
	}
	return useCaseView(repo.GetOnboardingUseCaseRow(row), defaultID), nil
}

// DeleteOnboardingUseCase retires a use case and every playbook of it.
// Organizations assigned one of those playbooks fall back to their setup
// task selection, since the wizard only follows a live playbook.
func DeleteOnboardingUseCase(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*admingen.AdminOnboardingUseCaseList, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin delete onboarding use case: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	deleted, err := queries.DeleteOnboardingUseCase(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("delete onboarding use case: %w", err)
	}
	if deleted == 0 {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err := queries.DeleteOnboardingPlaybooksOfUseCase(ctx, conv.ToNullUUID(id)); err != nil {
		return nil, fmt.Errorf("delete onboarding playbooks of use case: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delete onboarding use case: %w", err)
	}
	return ListOnboardingUseCases(ctx, db)
}

type playbookRow struct {
	ID               uuid.UUID
	UseCaseID        uuid.NullUUID
	OrganizationID   *string
	OrganizationName *string
	Name             string
	Description      string
	IsDefault        bool
	UseCaseSlug      *string
	UseCaseName      *string
}

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func playbookViews(ctx context.Context, queries *repo.Queries, rows []playbookRow) ([]*admingen.AdminOnboardingPlaybook, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	steps, err := queries.ListOnboardingPlaybookSteps(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list onboarding playbook steps: %w", err)
	}
	byPlaybook := make(map[uuid.UUID][]*admingen.AdminOnboardingPlaybookStep, len(rows))
	for _, step := range steps {
		byPlaybook[step.PlaybookID] = append(byPlaybook[step.PlaybookID], &admingen.AdminOnboardingPlaybookStep{Slug: step.Slug, Title: step.Title})
	}
	views := make([]*admingen.AdminOnboardingPlaybook, 0, len(rows))
	for _, row := range rows {
		list := byPlaybook[row.ID]
		if list == nil {
			list = []*admingen.AdminOnboardingPlaybookStep{}
		}
		views = append(views, &admingen.AdminOnboardingPlaybook{
			ID: row.ID.String(), UseCaseID: conv.FromNullableUUID(row.UseCaseID), UseCaseSlug: row.UseCaseSlug, UseCaseName: row.UseCaseName,
			OrganizationID: row.OrganizationID, OrganizationName: row.OrganizationName, Name: row.Name, Description: row.Description, IsDefault: row.IsDefault, Steps: list,
		})
	}
	return views, nil
}

// ListOnboardingPlaybooks reads every playbook, or, when an organization is
// named, the shared ones and its own.
func ListOnboardingPlaybooks(ctx context.Context, db repo.DBTX, organizationID *string) (*admingen.AdminOnboardingPlaybookList, error) {
	queries := repo.New(db)
	rows, err := queries.ListOnboardingPlaybooks(ctx, conv.PtrToPGText(organizationID))
	if err != nil {
		return nil, fmt.Errorf("list onboarding playbooks: %w", err)
	}
	plain := make([]playbookRow, 0, len(rows))
	for _, row := range rows {
		plain = append(plain, playbookRow{ID: row.ID, UseCaseID: row.UseCaseID, OrganizationID: conv.FromPGText[string](row.OrganizationID), OrganizationName: conv.FromPGText[string](row.OrganizationName), Name: row.Name, Description: row.Description, IsDefault: row.IsDefault, UseCaseSlug: conv.FromPGText[string](row.UseCaseSlug), UseCaseName: conv.FromPGText[string](row.UseCaseName)})
	}
	views, err := playbookViews(ctx, queries, plain)
	if err != nil {
		return nil, err
	}
	return &admingen.AdminOnboardingPlaybookList{Playbooks: views}, nil
}

// isNotFound reports whether err carries the not-found code.
func isNotFound(err error) bool {
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound
}

func loadPlaybook(ctx context.Context, queries *repo.Queries, id uuid.UUID) (*admingen.AdminOnboardingPlaybook, error) {
	row, err := queries.GetOnboardingPlaybook(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("load onboarding playbook: %w", err)
	}
	views, err := playbookViews(ctx, queries, []playbookRow{{ID: row.ID, UseCaseID: row.UseCaseID, OrganizationID: conv.FromPGText[string](row.OrganizationID), OrganizationName: conv.FromPGText[string](row.OrganizationName), Name: row.Name, Description: row.Description, IsDefault: row.IsDefault, UseCaseSlug: conv.FromPGText[string](row.UseCaseSlug), UseCaseName: conv.FromPGText[string](row.UseCaseName)}})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// playbookCards expands top-level slugs to the cards the wizard walks: a
// group contributes its cards in catalog order.
func playbookCards(topLevel []string) []string {
	cards := make([]string, 0, len(topLevel))
	for _, slug := range topLevel {
		if setupTaskGroupForKey(slug) != nil {
			for _, card := range setupTaskCatalog {
				if card.Parent == slug {
					cards = append(cards, card.Key)
				}
			}
			continue
		}
		cards = append(cards, slug)
	}
	return cards
}

// validatePlaybookSteps rejects a step list the wizard could not walk: an
// unknown or repeated slug, a card named without its group, or a step whose
// prerequisite is missing or comes later.
func validatePlaybookSteps(slugs []string) error {
	if len(slugs) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "a playbook needs at least one step")
	}
	position := make(map[string]int, len(slugs))
	for index, slug := range slugs {
		if _, dup := position[slug]; dup {
			return oops.E(oops.CodeBadRequest, nil, "step %q is listed twice", slug)
		}
		if card := setupTaskDefinitionForKey(slug); card != nil {
			if card.Parent != "" {
				return oops.E(oops.CodeBadRequest, nil, "step %q sits under %q; list the group instead", slug, card.Parent)
			}
		} else if setupTaskGroupForKey(slug) == nil {
			return oops.E(oops.CodeBadRequest, nil, "unknown step %q", slug)
		}
		position[slug] = index
	}
	topLevelOf := func(key string) string {
		if card := setupTaskDefinitionForKey(key); card != nil && card.Parent != "" {
			return card.Parent
		}
		return key
	}
	for _, key := range playbookCards(slugs) {
		card := setupTaskDefinitionForKey(key)
		for _, prerequisite := range card.Prerequisites {
			needed := topLevelOf(prerequisite)
			at, ok := position[needed]
			if !ok {
				return oops.E(oops.CodeBadRequest, nil, "step %q needs %q in the playbook", key, prerequisite)
			}
			if at > position[topLevelOf(key)] {
				return oops.E(oops.CodeBadRequest, nil, "step %q must come after %q", key, prerequisite)
			}
		}
	}
	return nil
}

func writePlaybookSteps(ctx context.Context, queries *repo.Queries, id uuid.UUID, slugs []string) error {
	if err := queries.DeleteOnboardingPlaybookSteps(ctx, id); err != nil {
		return fmt.Errorf("clear onboarding playbook steps: %w", err)
	}
	for index, slug := range slugs {
		inserted, err := queries.InsertOnboardingPlaybookStep(ctx, repo.InsertOnboardingPlaybookStepParams{PlaybookID: id, Slug: slug, Position: int32(index)})
		if err != nil {
			return fmt.Errorf("insert onboarding playbook step %q: %w", slug, err)
		}
		if inserted == 0 {
			return oops.E(oops.CodeBadRequest, nil, "step %q is not in the database yet; restart the server to mirror the catalog", slug)
		}
	}
	return nil
}

// CreateOnboardingPlaybook creates a playbook after checking its steps, and
// for a custom playbook its organization's stack.
func CreateOnboardingPlaybook(ctx context.Context, db *pgxpool.Pool, input OnboardingPlaybookInput) (*admingen.AdminOnboardingPlaybook, error) {
	if err := validateOnboardingText(input.Name, input.Description); err != nil {
		return nil, err
	}
	if err := validatePlaybookSteps(input.StepSlugs); err != nil {
		return nil, err
	}
	if (input.UseCaseID == nil) == (input.OrganizationID == nil) {
		return nil, oops.E(oops.CodeBadRequest, nil, "a playbook belongs to a use case or to an organization, not both")
	}
	if input.IsDefault && input.OrganizationID != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "a custom playbook cannot be a use case's default")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create onboarding playbook: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	if err := queries.LockOnboardingPlaybooks(ctx); err != nil {
		return nil, fmt.Errorf("lock onboarding playbooks: %w", err)
	}
	if input.UseCaseID != nil {
		if _, err := queries.GetOnboardingUseCase(ctx, *input.UseCaseID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "unknown use case")
			}
			return nil, fmt.Errorf("load onboarding use case: %w", err)
		}
	}
	if input.OrganizationID != nil {
		if err := rejectInapplicable(ctx, queries, *input.OrganizationID, input.StepSlugs); err != nil {
			return nil, err
		}
	}
	if input.IsDefault {
		// One default per use case: the previous one steps down first.
		if err := queries.ClearOnboardingDefaultPlaybook(ctx, repo.ClearOnboardingDefaultPlaybookParams{UseCaseID: nullUUID(input.UseCaseID), KeepID: uuid.Nil}); err != nil {
			return nil, fmt.Errorf("clear previous default playbook: %w", err)
		}
	}
	row, err := queries.CreateOnboardingPlaybook(ctx, repo.CreateOnboardingPlaybookParams{UseCaseID: nullUUID(input.UseCaseID), OrganizationID: conv.PtrToPGText(input.OrganizationID), Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), IsDefault: input.IsDefault})
	if err != nil {
		return nil, fmt.Errorf("create onboarding playbook: %w", err)
	}
	if err := writePlaybookSteps(ctx, queries, row.ID, input.StepSlugs); err != nil {
		return nil, err
	}
	view, err := loadPlaybook(ctx, queries, row.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create onboarding playbook: %w", err)
	}
	return view, nil
}

// UpdateOnboardingPlaybook replaces a playbook's name, description, default
// mark and steps.
func UpdateOnboardingPlaybook(ctx context.Context, db *pgxpool.Pool, id uuid.UUID, name, description string, isDefault bool, stepSlugs []string) (*admingen.AdminOnboardingPlaybook, error) {
	if err := validateOnboardingText(name, description); err != nil {
		return nil, err
	}
	if err := validatePlaybookSteps(stepSlugs); err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin update onboarding playbook: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	if err := queries.LockOnboardingPlaybooks(ctx); err != nil {
		return nil, fmt.Errorf("lock onboarding playbooks: %w", err)
	}
	current, err := loadPlaybook(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	if isDefault && current.OrganizationID != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "a custom playbook cannot be a use case's default")
	}
	if current.OrganizationID != nil {
		if err := rejectInapplicable(ctx, queries, *current.OrganizationID, stepSlugs); err != nil {
			return nil, err
		}
	}
	if isDefault {
		useCaseID, err := uuid.Parse(conv.PtrValOr(current.UseCaseID, ""))
		if err != nil {
			return nil, fmt.Errorf("parse use case id: %w", err)
		}
		if err := queries.ClearOnboardingDefaultPlaybook(ctx, repo.ClearOnboardingDefaultPlaybookParams{UseCaseID: conv.ToNullUUID(useCaseID), KeepID: id}); err != nil {
			return nil, fmt.Errorf("clear previous default playbook: %w", err)
		}
	}
	if _, err := queries.UpdateOnboardingPlaybook(ctx, repo.UpdateOnboardingPlaybookParams{ID: id, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), IsDefault: isDefault}); err != nil {
		return nil, fmt.Errorf("update onboarding playbook: %w", err)
	}
	if err := writePlaybookSteps(ctx, queries, id, stepSlugs); err != nil {
		return nil, err
	}
	view, err := loadPlaybook(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update onboarding playbook: %w", err)
	}
	return view, nil
}

// DeleteOnboardingPlaybook retires a playbook and returns what is left in
// its scope: the defaults, plus the custom ones of its organization.
func DeleteOnboardingPlaybook(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*admingen.AdminOnboardingPlaybookList, error) {
	queries := repo.New(db)
	current, err := loadPlaybook(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	if _, err := queries.DeleteOnboardingPlaybook(ctx, id); err != nil {
		return nil, fmt.Errorf("delete onboarding playbook: %w", err)
	}
	return ListOnboardingPlaybooks(ctx, db, current.OrganizationID)
}

// CloneOnboardingPlaybook copies a playbook into a custom one for an
// organization, checked against that organization's stack.
func CloneOnboardingPlaybook(ctx context.Context, db *pgxpool.Pool, organizationID string, playbookID uuid.UUID, name *string) (*admingen.AdminOnboardingPlaybook, error) {
	source, err := loadPlaybook(ctx, repo.New(db), playbookID)
	if err != nil {
		return nil, err
	}
	if source.OrganizationID != nil && *source.OrganizationID != organizationID {
		return nil, oops.E(oops.CodeBadRequest, nil, "that playbook belongs to another organization")
	}
	slugs := make([]string, 0, len(source.Steps))
	for _, step := range source.Steps {
		slugs = append(slugs, step.Slug)
	}
	// The copy belongs to the organization, so it leaves the use case behind.
	return CreateOnboardingPlaybook(ctx, db, OnboardingPlaybookInput{
		UseCaseID: nil, OrganizationID: &organizationID,
		Name: conv.PtrValOr(name, source.Name), Description: source.Description, IsDefault: false, StepSlugs: slugs,
	})
}

// stepApplicability says, for each top-level step, whether the organization's
// recorded stack supports it. A card applies when it has no methods, or when
// one of its methods belongs to a vendor in the stack (or to no vendor in
// particular) and the support matrix does not mark it not applicable on every
// platform of the stack. A group applies when any of its cards does.
func stepApplicability(ctx context.Context, queries *repo.Queries, organizationID string, topLevel []string) ([]*admingen.AdminOnboardingStepApplicability, error) {
	rows, err := queries.ListOrganizationOnboardingVendors(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list onboarding stack vendors: %w", err)
	}
	vendors := make([]string, 0, len(rows))
	for _, row := range rows {
		vendors = append(vendors, row.Vendor)
	}
	cards := playbookCards(topLevel)
	methods, err := queries.ListOnboardingStepMethodApplicability(ctx, repo.ListOnboardingStepMethodApplicabilityParams{Vendors: vendors, StepSlugs: cards})
	if err != nil {
		return nil, fmt.Errorf("list onboarding step applicability: %w", err)
	}
	type verdict struct {
		applies bool
		reason  string
	}
	cardVerdicts := make(map[string]verdict, len(cards))
	for _, card := range cards {
		cardVerdicts[card] = verdict{applies: true, reason: ""}
	}
	needs := make(map[string][]string, len(cards))
	// Every method is weighed: one that matches settles the card, and the
	// vendors of the ones that do not are what the reason names.
	for _, method := range methods {
		if _, first := needs[method.StepSlug]; !first {
			// The first method seen makes the card conditional.
			cardVerdicts[method.StepSlug] = verdict{applies: false, reason: ""}
			needs[method.StepSlug] = nil
		}
		anyVendor := method.MethodVendor == "Cross-platform" || method.MethodVendor == "Others"
		if (anyVendor || slices.Contains(vendors, method.MethodVendor)) && !method.NotApplicableEverywhere {
			cardVerdicts[method.StepSlug] = verdict{applies: true, reason: ""}
			continue
		}
		if !anyVendor && !slices.Contains(needs[method.StepSlug], method.MethodVendor) {
			needs[method.StepSlug] = append(needs[method.StepSlug], method.MethodVendor)
		}
	}
	for card, v := range cardVerdicts {
		if v.applies {
			continue
		}
		switch {
		case len(vendors) == 0:
			v.reason = "record the organization's stack first"
		case len(needs[card]) > 0:
			v.reason = "needs " + strings.Join(needs[card], " or ") + " in the stack"
		default:
			v.reason = "the support matrix marks its methods not applicable to this stack"
		}
		cardVerdicts[card] = v
	}
	result := make([]*admingen.AdminOnboardingStepApplicability, 0, len(topLevel))
	for _, slug := range topLevel {
		if group := setupTaskGroupForKey(slug); group != nil {
			applies := false
			reasons := make([]string, 0)
			for _, card := range setupTaskCatalog {
				if card.Parent != slug {
					continue
				}
				v := cardVerdicts[card.Key]
				if v.applies {
					applies = true
				} else {
					reasons = append(reasons, card.Title+": "+v.reason)
				}
			}
			reason := ""
			if !applies {
				reason = strings.Join(reasons, "; ")
			}
			result = append(result, &admingen.AdminOnboardingStepApplicability{Slug: slug, Title: group.Title, Applies: applies, Reason: reason})
			continue
		}
		card := setupTaskDefinitionForKey(slug)
		v := cardVerdicts[slug]
		result = append(result, &admingen.AdminOnboardingStepApplicability{Slug: slug, Title: card.Title, Applies: v.applies, Reason: v.reason})
	}
	return result, nil
}

func rejectInapplicable(ctx context.Context, queries *repo.Queries, organizationID string, topLevel []string) error {
	applicability, err := stepApplicability(ctx, queries, organizationID, topLevel)
	if err != nil {
		return err
	}
	problems := make([]string, 0)
	for _, step := range applicability {
		if !step.Applies {
			problems = append(problems, step.Title+" ("+step.Reason+")")
		}
	}
	if len(problems) > 0 {
		return oops.E(oops.CodeBadRequest, nil, "the stack does not support: %s", strings.Join(problems, "; "))
	}
	return nil
}

// LoadOrganizationOnboardingPlaybook reads the playbook assigned to an
// organization and how each of its steps fares against the recorded stack.
func LoadOrganizationOnboardingPlaybook(ctx context.Context, db repo.DBTX, organizationID string) (*admingen.AdminOrganizationOnboardingPlaybook, error) {
	queries := repo.New(db)
	row, err := queries.GetOrganizationOnboardingPlaybookID(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("load onboarding playbook assignment: %w", err)
	}
	result := &admingen.AdminOrganizationOnboardingPlaybook{OrganizationID: organizationID, Playbook: nil, Applicability: []*admingen.AdminOnboardingStepApplicability{}}
	if !row.PlaybookID.Valid {
		return result, nil
	}
	playbook, err := loadPlaybook(ctx, queries, row.PlaybookID.UUID)
	if err != nil {
		if isNotFound(err) {
			// A retired playbook: the organization is back on its selection.
			return result, nil
		}
		return nil, err
	}
	slugs := make([]string, 0, len(playbook.Steps))
	for _, step := range playbook.Steps {
		slugs = append(slugs, step.Slug)
	}
	applicability, err := stepApplicability(ctx, queries, organizationID, slugs)
	if err != nil {
		return nil, err
	}
	result.Playbook = playbook
	result.Applicability = applicability
	return result, nil
}

func playbookSnapshot(playbook *admingen.AdminOnboardingPlaybook) *audit.OrganizationOnboardingPlaybookSnapshot {
	if playbook == nil {
		return nil
	}
	slugs := make([]string, 0, len(playbook.Steps))
	for _, step := range playbook.Steps {
		slugs = append(slugs, step.Slug)
	}
	// A customer's own playbook has no use case, so the snapshot leaves it out.
	return &audit.OrganizationOnboardingPlaybookSnapshot{PlaybookID: playbook.ID, Name: playbook.Name, UseCase: conv.PtrValOr(playbook.UseCaseSlug, ""), StepSlugs: slugs}
}

// AssignOrganizationOnboardingPlaybook assigns a playbook to an organization,
// or clears it when playbookID is nil. A playbook whose steps the recorded
// stack does not support is rejected, naming them. Its caller authenticates
// staff or an organization admin.
func AssignOrganizationOnboardingPlaybook(ctx context.Context, db *pgxpool.Pool, logger *audit.Logger, organizationID string, playbookID *uuid.UUID, actor urn.Principal, displayName *string) (*admingen.AdminOrganizationOnboardingPlaybook, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin assign onboarding playbook: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	org, err := queries.LockOrganizationForSetupTaskUpdate(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("lock onboarding playbook organization: %w", err)
	}
	before, err := LoadOrganizationOnboardingPlaybook(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	var assigned uuid.NullUUID
	if playbookID != nil {
		playbook, err := loadPlaybook(ctx, queries, *playbookID)
		if err != nil {
			if isNotFound(err) {
				return nil, oops.E(oops.CodeBadRequest, nil, "unknown playbook")
			}
			return nil, err
		}
		if playbook.OrganizationID != nil && *playbook.OrganizationID != organizationID {
			return nil, oops.E(oops.CodeBadRequest, nil, "that playbook belongs to another organization")
		}
		slugs := make([]string, 0, len(playbook.Steps))
		for _, step := range playbook.Steps {
			slugs = append(slugs, step.Slug)
		}
		if err := rejectInapplicable(ctx, queries, organizationID, slugs); err != nil {
			return nil, err
		}
		assigned = uuid.NullUUID{UUID: *playbookID, Valid: true}
	}
	if err := queries.SetOrganizationOnboardingPlaybook(ctx, repo.SetOrganizationOnboardingPlaybookParams{OrganizationID: organizationID, PlaybookID: assigned}); err != nil {
		return nil, fmt.Errorf("assign onboarding playbook: %w", err)
	}
	after, err := LoadOrganizationOnboardingPlaybook(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	if err := logger.LogOrganizationOnboardingPlaybookAssigned(ctx, tx, audit.LogOrganizationOnboardingPlaybookAssignedEvent{
		OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName,
		OrganizationName: org.Name, OrganizationSlug: org.Slug,
		PlaybookSnapshotBefore: playbookSnapshot(before.Playbook), PlaybookSnapshotAfter: playbookSnapshot(after.Playbook),
	}); err != nil {
		return nil, fmt.Errorf("audit onboarding playbook: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit assign onboarding playbook: %w", err)
	}
	return after, nil
}
