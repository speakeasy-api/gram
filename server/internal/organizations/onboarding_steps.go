package organizations

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	admingen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// onboardingStepRecord is one row of the step mirror: a group or a card from
// the catalog, in wizard order.
type onboardingStepRecord struct {
	Slug            string
	Title           string
	Description     string
	Parent          string
	Completion      setupTaskCompletion
	HiddenByDefault bool
	Methods         []string
	Prerequisites   []string
}

// onboardingStepRecords flattens the groups and cards into wizard order: a
// group takes the place of its first card, and its cards follow it.
func onboardingStepRecords() []onboardingStepRecord {
	records := make([]onboardingStepRecord, 0, len(setupTaskCatalog)+len(setupTaskGroups))
	placed := make(map[string]int, len(setupTaskGroups))
	for _, card := range setupTaskCatalog {
		if card.Parent != "" {
			index, ok := placed[card.Parent]
			if !ok {
				group := setupTaskGroupForKey(card.Parent)
				index = len(records)
				placed[card.Parent] = index
				records = append(records, onboardingStepRecord{Slug: group.Key, Title: group.Title, Description: group.Description, Parent: "", Completion: setupTaskCompletionChildren, HiddenByDefault: true, Methods: nil, Prerequisites: nil})
			}
			if !card.HiddenByDefault {
				records[index].HiddenByDefault = false
			}
		}
		records = append(records, onboardingStepRecord{Slug: card.Key, Title: card.Title, Description: card.Description, Parent: card.Parent, Completion: card.Completion, HiddenByDefault: card.HiddenByDefault, Methods: card.Methods, Prerequisites: card.Prerequisites})
	}
	return records
}

// SyncOnboardingSteps mirrors the setup task catalog and its groups into
// onboarding_steps so playbooks can reference them and staff can read them.
// Code owns every column: a slug the code no longer defines is retired, and
// methods and prerequisites are replaced. Runs at start-up under an advisory
// lock so several replicas can start at once. Integration methods come from
// the support matrix, so the matrix is seeded first.
func SyncOnboardingSteps(ctx context.Context, db *pgxpool.Pool) error {
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockOnboardingSteps(ctx); err != nil {
			return fmt.Errorf("lock onboarding steps: %w", err)
		}
		records := onboardingStepRecords()
		ids := make(map[string]uuid.UUID, len(records))
		slugs := make([]string, 0, len(records))
		for position, record := range records {
			id, err := q.UpsertOnboardingStep(ctx, repo.UpsertOnboardingStepParams{
				Slug:            record.Slug,
				Title:           record.Title,
				Description:     record.Description,
				Completion:      string(record.Completion),
				HiddenByDefault: record.HiddenByDefault,
				SortOrder:       int32(position),
			})
			if err != nil {
				return fmt.Errorf("upsert onboarding step %q: %w", record.Slug, err)
			}
			ids[record.Slug] = id
			slugs = append(slugs, record.Slug)
		}
		if err := q.RetireOnboardingStepsNotIn(ctx, slugs); err != nil {
			return fmt.Errorf("retire onboarding steps: %w", err)
		}
		for _, record := range records {
			id := ids[record.Slug]
			var parent uuid.NullUUID
			if record.Parent != "" {
				parent = uuid.NullUUID{UUID: ids[record.Parent], Valid: true}
			}
			if err := q.SetOnboardingStepParent(ctx, repo.SetOnboardingStepParentParams{ID: id, ParentStepID: parent}); err != nil {
				return fmt.Errorf("set onboarding step parent %q: %w", record.Slug, err)
			}
			if err := q.DeleteOnboardingStepMethods(ctx, id); err != nil {
				return fmt.Errorf("clear onboarding step methods %q: %w", record.Slug, err)
			}
			for _, method := range record.Methods {
				inserted, err := q.InsertOnboardingStepMethod(ctx, repo.InsertOnboardingStepMethodParams{StepID: id, MethodSlug: method})
				if err != nil {
					return fmt.Errorf("insert onboarding step method %q for %q: %w", method, record.Slug, err)
				}
				if inserted == 0 {
					return fmt.Errorf("onboarding step %q names integration method %q that the support matrix does not define", record.Slug, method)
				}
			}
			if err := q.DeleteOnboardingStepDependencies(ctx, id); err != nil {
				return fmt.Errorf("clear onboarding step dependencies %q: %w", record.Slug, err)
			}
			for _, requires := range record.Prerequisites {
				if err := q.InsertOnboardingStepDependency(ctx, repo.InsertOnboardingStepDependencyParams{StepID: id, RequiresSlug: requires}); err != nil {
					return fmt.Errorf("insert onboarding step dependency %q for %q: %w", requires, record.Slug, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sync onboarding steps: %w", err)
	}
	return nil
}

// ListOnboardingSteps reads the mirrored steps in wizard order for the admin
// dashboard, with each step's parent, methods and prerequisites by slug.
func ListOnboardingSteps(ctx context.Context, db repo.DBTX) ([]*admingen.AdminOnboardingStep, error) {
	rows, err := repo.New(db).ListOnboardingSteps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list onboarding steps: %w", err)
	}
	steps := make([]*admingen.AdminOnboardingStep, 0, len(rows))
	for _, row := range rows {
		steps = append(steps, &admingen.AdminOnboardingStep{
			Slug:            row.Slug,
			Title:           row.Title,
			Description:     row.Description,
			ParentSlug:      conv.FromPGText[string](row.ParentSlug),
			Completion:      row.Completion,
			HiddenByDefault: row.HiddenByDefault,
			MethodSlugs:     row.MethodSlugs,
			Requires:        row.RequiresSlugs,
		})
	}
	return steps, nil
}
