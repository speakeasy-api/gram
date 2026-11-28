package demoseed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/trials"
)

// Each status set exceeds the admin list's 50-row page size.
const (
	AdminSeedOrganizationsPerStatus = 60
	AdminSeedOrganizationCount      = 2 * AdminSeedOrganizationsPerStatus
	AdminSeedSearchName             = "Fictional Admin Lab"
)

// validateAdminSeedTarget is checked before opening a connection. Match the
// repository's local environment gate, plus fail closed on non-development DBs
// and every non-loopback primary/fallback target. Never include the DSN in errors.
func validateAdminSeedTarget(environment string, config *pgx.ConnConfig) error {
	if environment != "local" || config.Database != "gram" || config.User != "gram" {
		return errors.New("admin seed requires environment=local and the gram development database/user")
	}
	hosts := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("admin seed requires loopback-only PostgreSQL targets (including fallbacks)")
		}
	}
	return nil
}

type adminSeedFixture struct {
	ID, Name, Slug string
	Members        int
	Disabled       bool
	CreatedAt      time.Time
}

func adminSeedFixtures(now time.Time) []adminSeedFixture {
	now = now.UTC()
	midnight := now.Truncate(24 * time.Hour)
	dates := []time.Time{now, now.Add(-time.Hour)}
	// Calendar presets include today: an N-day window starts at UTC midnight
	// N-1 days ago. Also bracket today's start and a custom range's exclusive
	// end (midnight after an end date three days ago).
	boundaries := []time.Time{midnight, midnight.AddDate(0, 0, -2)}
	for _, days := range []int{7, 14, 30} {
		boundaries = append(boundaries, midnight.AddDate(0, 0, -(days-1)), now.AddDate(0, 0, -days))
	}
	for _, boundary := range boundaries {
		for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
			candidate := boundary.Add(offset)
			// At midnight, the after-today-start fixture would otherwise be future.
			if !candidate.After(now) {
				dates = append(dates, candidate)
			}
		}
	}
	for _, days := range []int{60, 90, 180, 365} {
		dates = append(dates, midnight.AddDate(0, 0, -days))
	}
	counts := []int{0, 1, 5, 10, 25, 100}
	fixtures := make([]adminSeedFixture, AdminSeedOrganizationCount)
	for i := range fixtures {
		fixtures[i] = adminSeedFixture{
			ID:      fmt.Sprintf("org_local_admin_fixture_%02d", i+1),
			Name:    fmt.Sprintf("%s %02d", AdminSeedSearchName, i+1),
			Slug:    fmt.Sprintf("fictional-admin-lab-%02d", i+1),
			Members: counts[i%len(counts)], Disabled: i >= AdminSeedOrganizationsPerStatus, CreatedAt: dates[(i%AdminSeedOrganizationsPerStatus)%len(dates)],
		}
	}
	fixtures[60].Name = fixtures[0].Name // Duplicate display names, distinct canonical targets.
	return fixtures
}

// AdminSeedSharedCardFingerprint is the fictional card fingerprint two trial
// fixtures offered: it started one organization's trial and was rejected for
// the other, so the staff view shows a card reused across organizations.
const AdminSeedSharedCardFingerprint = "fp_admin_seed_example_shared_card"

// AdminSeedPrepaidCardFingerprint is the fictional fingerprint of the prepaid
// card a trial fixture offered.
const AdminSeedPrepaidCardFingerprint = "fp_admin_seed_example_prepaid_card"

// adminSeedTrialFixture is a fictional organization in a self-serve free trial
// state. Card is nil for an organization that never offered one.
type adminSeedTrialFixture struct {
	ID, Name, Slug string
	CreatedAt      time.Time
	Card           *adminSeedTrialCard
}

// adminSeedTrialCard is the latest card a fixture offered, stored in its
// trial_payment_methods row. An empty RejectionReason means the card started a
// running enterprise trial ending at TrialEndsAt; otherwise the organization
// has no trial and still awaits a card. Every identifier is fictional and
// marked as an admin seed example.
type adminSeedTrialCard struct {
	CustomerID, PaymentMethodID, CheckoutSessionID, Fingerprint string
	Funding, RejectionReason                                    string
	TrialEndsAt                                                 time.Time
}

// StartedTrial reports whether the card started the fixture's trial.
func (c *adminSeedTrialCard) StartedTrial() bool {
	return c != nil && c.RejectionReason == ""
}

// adminSeedTrialFixtures covers the card-gated free trial: an organization
// that waits for a card, one whose trial started with a card, one that offered
// the same card and was rejected because it already started that trial, and
// one whose prepaid card was rejected.
func adminSeedTrialFixtures(now time.Time) []adminSeedTrialFixture {
	now = now.UTC()
	card := func(suffix, fingerprint, funding, rejection string) *adminSeedTrialCard {
		return &adminSeedTrialCard{
			CustomerID:        "cus_admin_seed_example_" + suffix,
			PaymentMethodID:   "pm_admin_seed_example_" + suffix,
			CheckoutSessionID: "cs_admin_seed_example_" + suffix,
			Fingerprint:       fingerprint,
			Funding:           funding,
			RejectionReason:   rejection,
			TrialEndsAt:       time.Time{},
		}
	}
	started, reused, prepaid := now.AddDate(0, 0, -5), now.AddDate(0, 0, -2), now.AddDate(0, 0, -3)
	startedCard := card("a", AdminSeedSharedCardFingerprint, "credit", "")
	startedCard.TrialEndsAt = started.AddDate(0, 0, 14)
	return []adminSeedTrialFixture{
		{ID: "org_local_admin_trial_pending", Name: "Fictional Trial Lab Awaiting Card", Slug: "fictional-trial-lab-awaiting-card", CreatedAt: now.AddDate(0, 0, -1), Card: nil},
		{ID: "org_local_admin_trial_card_a", Name: "Fictional Trial Lab Shared Card A", Slug: "fictional-trial-lab-shared-card-a", CreatedAt: started, Card: startedCard},
		{ID: "org_local_admin_trial_card_b", Name: "Fictional Trial Lab Shared Card B", Slug: "fictional-trial-lab-shared-card-b", CreatedAt: reused, Card: card("b", AdminSeedSharedCardFingerprint, "credit", trials.CardRejectionUsed)},
		{ID: "org_local_admin_trial_prepaid", Name: "Fictional Trial Lab Prepaid Card", Slug: "fictional-trial-lab-prepaid-card", CreatedAt: prepaid, Card: card("prepaid", AdminSeedPrepaidCardFingerprint, "prepaid", trials.CardRejectionPrepaid)},
	}
}

// Dedicated fictional users cover lifecycle and overflow without changing the
// organization-list fixtures' original membership distribution.
type adminSeedUserFixture struct {
	ID, Name                                            string
	Organizations                                       []string
	Deleted, WorkosDeleted, DeletedMembership, HasLogin bool
}

func adminSeedUserFixtures() []adminSeedUserFixture {
	return []adminSeedUserFixture{
		{ID: "user_local_admin_edge_zero", Name: "", Organizations: nil, Deleted: false, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_multi", Name: "Fictional 100%_Literal Member", Organizations: []string{"org_local_admin_fixture_01", "org_local_admin_fixture_02", "org_local_admin_fixture_03", "org_local_admin_fixture_04", "org_local_admin_fixture_61"}, Deleted: false, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_deleted", Name: "Fictional Deleted Member", Deleted: true, Organizations: nil, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_workos_deleted", Name: "Fictional Externally Deleted Member", WorkosDeleted: true, Organizations: nil, Deleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_membership_deleted", Name: "Fictional Former Member", Organizations: []string{"org_local_admin_fixture_01"}, DeletedMembership: true, Deleted: false, WorkosDeleted: false, HasLogin: false},
		{ID: "user_local_admin_edge_login", Name: "Fictional Returning Member", HasLogin: true, Organizations: nil, Deleted: false, WorkosDeleted: false, DeletedMembership: false},
	}
}

// RunAdminSeed is deliberately independent of Run and RunLocalFixtures. It
// creates no projects, developer memberships, credentials or external accounts.
func RunAdminSeed(ctx context.Context, environment, databaseURL string) error {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid admin seed database configuration")
	}
	if err := validateAdminSeedTarget(environment, config.ConnConfig); err != nil {
		return err
	}
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect admin seed database: %w", err)
	}
	defer db.Close()
	return runAdminSeed(ctx, db, time.Now())
}

func runAdminSeed(ctx context.Context, db *pgxpool.Pool, now time.Time) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin admin seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := repo.New(tx)
	// Serialize concurrent invocations; all fixture writes commit atomically.
	if err = q.AdminSeedLock(ctx); err != nil {
		return fmt.Errorf("serialize admin seed: %w", err)
	}
	// The advisory lock only serializes seeders. Lock the local-only fixture
	// tables against other writers too, including inserts into currently absent
	// reserved IDs, so ownership cannot change between validation and upsert.
	if err := q.LockAdminSeedIdentities(ctx); err != nil {
		return fmt.Errorf("lock admin seed identities: %w", err)
	}
	fixtures := adminSeedFixtures(now)
	// Validate every existing identity before the first write. Names, dates,
	// disabled/deleted states are refreshed deliberately; stable fictional slugs
	// and emails, no external identity, and no unrelated memberships mark ownership.
	for i, fixture := range fixtures {
		collision, err := q.AdminSeedOrganizationCollision(ctx, repo.AdminSeedOrganizationCollisionParams{OrganizationID: fixture.ID, Slug: fixture.Slug})
		if err != nil {
			return fmt.Errorf("validate fictional organization identity: %w", err)
		}
		if collision {
			return errors.New("admin seed organization identity collision")
		}
		userIDs := make([]string, fixture.Members)
		for member := range userIDs {
			userIDs[member] = fmt.Sprintf("user_local_admin_fixture_%02d_%03d", i+1, member+1)
		}
		collision, err = q.AdminSeedMemberCollision(ctx, repo.AdminSeedMemberCollisionParams{UserIds: userIDs, OrganizationID: fixture.ID})
		if err != nil {
			return fmt.Errorf("validate fictional member identity: %w", err)
		}
		if collision {
			return errors.New("admin seed member identity collision")
		}
	}
	trialFixtures := adminSeedTrialFixtures(now)
	for _, fixture := range trialFixtures {
		collision, err := q.AdminSeedOrganizationCollision(ctx, repo.AdminSeedOrganizationCollisionParams{OrganizationID: fixture.ID, Slug: fixture.Slug})
		if err != nil {
			return fmt.Errorf("validate fictional trial organization identity: %w", err)
		}
		if collision {
			return errors.New("admin seed trial organization identity collision")
		}
	}
	updatedAt := conv.ToPGTimestamptz(now)
	if err := seedAdminTrialFixtures(ctx, q, trialFixtures, updatedAt); err != nil {
		return err
	}
	for i, fixture := range fixtures {
		var disabledAt *time.Time
		if fixture.Disabled {
			disabledAt = &now
		}
		err = q.AdminSeedUpsertOrganization(ctx, repo.AdminSeedUpsertOrganizationParams{
			ID:         fixture.ID,
			Name:       fixture.Name,
			Slug:       fixture.Slug,
			CreatedAt:  conv.ToPGTimestamptz(fixture.CreatedAt),
			DisabledAt: conv.PtrToPGTimestamptz(disabledAt),
			UpdatedAt:  updatedAt,
		})
		if err != nil {
			return fmt.Errorf("seed fictional organization: %w", err)
		}
		for member := 1; member <= fixture.Members; member++ {
			userID := fmt.Sprintf("user_local_admin_fixture_%02d_%03d", i+1, member)
			err = q.AdminSeedUpsertMember(ctx, repo.AdminSeedUpsertMemberParams{
				ID:          userID,
				Email:       userID + "@admin-seed.invalid",
				DisplayName: "Fictional Admin Member",
				UpdatedAt:   updatedAt,
			})
			if err != nil {
				return fmt.Errorf("seed fictional member: %w", err)
			}
			err = q.AdminSeedUpsertMembership(ctx, repo.AdminSeedUpsertMembershipParams{
				ID:             int64(-8900000 - (i+1)*1000 - member),
				OrganizationID: fixture.ID,
				UserID:         userID,
				UpdatedAt:      updatedAt,
			})
			if err != nil {
				return fmt.Errorf("seed fictional membership: %w", err)
			}
		}
	}
	for i, fixture := range adminSeedUserFixtures() {
		orgIDs := fixture.Organizations
		if orgIDs == nil {
			orgIDs = []string{}
		}
		collision, err := q.AdminSeedEdgeUserCollision(ctx, repo.AdminSeedEdgeUserCollisionParams{UserID: fixture.ID, OrganizationIds: orgIDs})
		if err != nil {
			return fmt.Errorf("validate fictional edge user: %w", err)
		}
		if collision {
			return errors.New("admin seed edge user identity collision")
		}
		var deletedAt, workosDeletedAt, lastLogin *time.Time
		if fixture.Deleted {
			deletedAt = &now
		}
		if fixture.WorkosDeleted {
			workosDeletedAt = &now
		}
		if fixture.HasLogin {
			lastLogin = &now
		}
		if err := q.AdminSeedUpsertEdgeUser(ctx, repo.AdminSeedUpsertEdgeUserParams{
			ID:              fixture.ID,
			Email:           fixture.ID + "@admin-seed.invalid",
			DisplayName:     fixture.Name,
			DeletedAt:       conv.PtrToPGTimestamptz(deletedAt),
			WorkosDeletedAt: conv.PtrToPGTimestamptz(workosDeletedAt),
			LastLogin:       conv.PtrToPGTimestamptz(lastLogin),
			UpdatedAt:       updatedAt,
		}); err != nil {
			return fmt.Errorf("seed fictional edge user: %w", err)
		}
		for j, orgID := range orgIDs {
			var membershipDeletedAt *time.Time
			if fixture.DeletedMembership {
				membershipDeletedAt = &now
			}
			if err := q.AdminSeedUpsertEdgeMembership(ctx, repo.AdminSeedUpsertEdgeMembershipParams{
				ID:             int64(-9200000 - i*100 - j),
				OrganizationID: orgID,
				UserID:         fixture.ID,
				DeletedAt:      conv.PtrToPGTimestamptz(membershipDeletedAt),
				UpdatedAt:      updatedAt,
			}); err != nil {
				return fmt.Errorf("seed fictional edge membership: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin seed: %w", err)
	}
	return nil
}

// seedAdminTrialFixtures writes the free trial fixtures. Each one is reset to
// its fixture state, so a card or trial a developer added locally does not
// survive a rerun. Every write is scoped to one fictional organization.
func seedAdminTrialFixtures(ctx context.Context, q *repo.Queries, fixtures []adminSeedTrialFixture, updatedAt pgtype.Timestamptz) error {
	for _, fixture := range fixtures {
		accountType, whitelisted := string(billing.TierBase), false
		if fixture.Card.StartedTrial() {
			accountType, whitelisted = string(billing.TierEnterprise), true
		}
		if err := q.AdminSeedUpsertTrialOrganization(ctx, repo.AdminSeedUpsertTrialOrganizationParams{
			ID:              fixture.ID,
			Name:            fixture.Name,
			Slug:            fixture.Slug,
			GramAccountType: accountType,
			Whitelisted:     whitelisted,
			CreationSource:  orgprovision.SourceSignup,
			CreatedAt:       conv.ToPGTimestamptz(fixture.CreatedAt),
			UpdatedAt:       updatedAt,
		}); err != nil {
			return fmt.Errorf("seed fictional trial organization: %w", err)
		}

		if fixture.Card == nil {
			if err := q.AdminSeedClearTrialPaymentMethod(ctx, fixture.ID); err != nil {
				return fmt.Errorf("clear fictional trial card: %w", err)
			}
		} else {
			var rejection pgtype.Text
			if !fixture.Card.StartedTrial() {
				rejection = conv.ToPGText(fixture.Card.RejectionReason)
			}
			if err := q.AdminSeedUpsertTrialPaymentMethod(ctx, repo.AdminSeedUpsertTrialPaymentMethodParams{
				OrganizationID:          fixture.ID,
				StripeCustomerID:        fixture.Card.CustomerID,
				StripePaymentMethodID:   fixture.Card.PaymentMethodID,
				CardFingerprint:         fixture.Card.Fingerprint,
				CardFunding:             fixture.Card.Funding,
				StripeCheckoutSessionID: fixture.Card.CheckoutSessionID,
				RejectionReason:         rejection,
				OfferedAt:               conv.ToPGTimestamptz(fixture.CreatedAt),
			}); err != nil {
				return fmt.Errorf("seed fictional trial card: %w", err)
			}
		}

		if !fixture.Card.StartedTrial() {
			if err := q.AdminSeedClearTrial(ctx, fixture.ID); err != nil {
				return fmt.Errorf("clear fictional trial: %w", err)
			}
			continue
		}
		if err := q.AdminSeedUpsertTrial(ctx, repo.AdminSeedUpsertTrialParams{
			OrganizationID: fixture.ID,
			Tier:           string(billing.TierEnterprise),
			EndsAt:         conv.ToPGTimestamptz(fixture.Card.TrialEndsAt),
			UpdatedAt:      updatedAt,
		}); err != nil {
			return fmt.Errorf("seed fictional trial: %w", err)
		}
	}
	return nil
}
