package growthsignals

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Emitter ships activities to PostHog.
//
// It is the single door every producer goes through, so the rules that must
// hold for all of them — the demo organization never reports, a failed lookup
// narrows an event instead of dropping it, a failed capture never reaches the
// caller — are enforced once here rather than at each emission.
type Emitter struct {
	logger   *slog.Logger
	client   PostHogClient
	enricher Enricher

	// siteURL is the dashboard's base URL, used to build the fallback
	// dashboard_url for activities that carry no subject page of their own.
	siteURL *url.URL
}

func NewEmitter(logger *slog.Logger, client PostHogClient, enricher Enricher, siteURL *url.URL) *Emitter {
	componentLogger := logger.With(attr.SlogComponent("growth-signals"))
	if siteURL == nil {
		// Every activity would then report no dashboard link, and a Slack
		// destination that renders one as a button omits it. Worth saying once
		// at startup rather than leaving it to be noticed in the channel.
		componentLogger.WarnContext(context.Background(), "growth signals have no site url; activities will carry no dashboard link")
	}

	return &Emitter{
		logger:   componentLogger,
		client:   client,
		enricher: enricher,
		siteURL:  siteURL,
	}
}

// Emit enriches one activity and captures it.
//
// It returns nothing. Producers call this from paths whose real work has
// already succeeded — a project was created, a member joined — and a dropped
// analytics event must never be able to fail that work. Everything that goes
// wrong is logged here instead.
func (e *Emitter) Emit(ctx context.Context, event ActivityEvent) {
	// A nil emitter reports nothing and says so quietly. Callers that have no
	// PostHog wiring — tests, and commands that construct a service without the
	// analytics stack — should not each have to guard the call, and analytics
	// must never be the reason a request path panics.
	if e == nil {
		return
	}

	if event.Activity == "" || event.Activity == ActivitySkip {
		return
	}

	// The demo organization is reseeded daily from a fixture, so every one of
	// its mutations is a scripted one. Reporting them would bury the real
	// signal under a burst of identical activity every morning.
	if event.OrganizationID == constants.DemoOrganizationID {
		return
	}

	captured := BuildEvent(event, e.enrich(ctx, event), e.siteURL)

	if err := e.client.CaptureEvent(ctx, captured.Name, captured.DistinctID, captured.Properties); err != nil {
		e.logger.ErrorContext(ctx, "capture growth activity",
			attr.SlogError(err),
			attr.SlogEvent(string(event.Activity)),
			attr.SlogOrganizationID(event.OrganizationID),
		)
	}
}

// enrich resolves the ids an event carries into the names it reports. Each
// lookup stands alone: one that fails costs its own properties and nothing
// else, so a degraded event still carries everything that did resolve.
func (e *Emitter) enrich(ctx context.Context, event ActivityEvent) Enrichment {
	enrichment := Enrichment{
		Organization: OrganizationDetails{Slug: "", Name: ""},
		Project:      ProjectDetails{Slug: "", Name: ""},
		ActorEmail:   event.ActorEmail,
	}

	if event.OrganizationID != "" {
		organization, err := e.enricher.Organization(ctx, event.OrganizationID)
		if err != nil {
			e.logger.WarnContext(ctx, "resolve organization for growth activity",
				attr.SlogError(err),
				attr.SlogOrganizationID(event.OrganizationID),
			)
		} else {
			enrichment.Organization = organization
		}
	}

	if event.ProjectID != uuid.Nil {
		project, err := e.enricher.Project(ctx, event.ProjectID)
		if err != nil {
			e.logger.WarnContext(ctx, "resolve project for growth activity",
				attr.SlogError(err),
				attr.SlogProjectID(event.ProjectID.String()),
			)
		} else {
			enrichment.Project = project
		}
	}

	if enrichment.ActorEmail == "" {
		enrichment.ActorEmail = e.resolveActorEmail(ctx, event)
	}

	return enrichment
}

// resolveActorEmail finds the email address of the person behind an activity,
// or returns empty when there is none.
//
// An email principal already is the address. A user principal needs a lookup,
// except for the reserved subject-set id, which stands for every user rather
// than for one. A role principal has no person behind it at all, and the event
// falls back to the organization as its distinct id.
func (e *Emitter) resolveActorEmail(ctx context.Context, event ActivityEvent) string {
	if event.ActorID == "" {
		return ""
	}

	switch event.ActorType {
	case urn.PrincipalTypeEmail:
		return event.ActorID
	case urn.PrincipalTypeUser:
		if event.ActorID == urn.AllUsersPrincipalID {
			return ""
		}

		email, err := e.enricher.UserEmail(ctx, event.ActorID)
		if err != nil {
			e.logger.WarnContext(ctx, "resolve actor email for growth activity",
				attr.SlogError(err),
				attr.SlogUserID(event.ActorID),
			)
			return ""
		}

		return email
	case urn.PrincipalTypeRole:
		return ""
	default:
		return ""
	}
}
