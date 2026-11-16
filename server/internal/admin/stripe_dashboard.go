package admin

import (
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const stripeDashboardOrigin = "https://dashboard.stripe.com"

// SetStripeDashboardMode scopes Stripe dashboard links to the mode of the
// deployment's Stripe API key. Test objects only resolve under /test, and the
// key prefix is the only signal for which mode the stored IDs belong to. The
// key itself is not retained.
func (s *Service) SetStripeDashboardMode(apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	switch {
	case strings.HasPrefix(apiKey, "sk_live_"), strings.HasPrefix(apiKey, "rk_live_"):
		s.stripeDashboardBase = stripeDashboardOrigin
	case strings.HasPrefix(apiKey, "sk_test_"), strings.HasPrefix(apiKey, "rk_test_"):
		s.stripeDashboardBase = stripeDashboardOrigin + "/test"
	default:
		s.stripeDashboardBase = ""
	}
}

func (s *Service) stripeCustomerDashboardURL(customerID pgtype.Text) *string {
	return s.stripeDashboardURL("customers", customerID)
}

func (s *Service) stripeSubscriptionDashboardURL(subscriptionID pgtype.Text) *string {
	return s.stripeDashboardURL("subscriptions", subscriptionID)
}

// stripeDashboardURL is nil unless both the dashboard mode and the object ID
// are known.
func (s *Service) stripeDashboardURL(collection string, id pgtype.Text) *string {
	if s.stripeDashboardBase == "" || !id.Valid || id.String == "" {
		return nil
	}
	link := s.stripeDashboardBase + "/" + collection + "/" + url.PathEscape(id.String)
	return &link
}
