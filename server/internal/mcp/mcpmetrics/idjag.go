package mcpmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// IDJAGExchangeOutcome is the closed set of ID-JAG token exchange outcomes.
type IDJAGExchangeOutcome string

const (
	// IDJAGExchangeAccepted: the exchange committed a session.
	IDJAGExchangeAccepted IDJAGExchangeOutcome = "accepted"
	// IDJAGExchangeRejected: the exchange was refused with a 4xx OAuth error.
	IDJAGExchangeRejected IDJAGExchangeOutcome = "rejected"
	// IDJAGExchangeError: the exchange failed with a server error.
	IDJAGExchangeError IDJAGExchangeOutcome = "error"
)

// IDJAGRejectionReason is the closed set of reasons [Metrics] records on
// oauth.id_jag.exchange under gram.oauth.id_jag.rejection_reason.
type IDJAGRejectionReason string

const (
	// IDJAGRejectionNone: the exchange was not rejected.
	IDJAGRejectionNone IDJAGRejectionReason = ""
	// IDJAGRejectionInvalidRequest: the token request was malformed.
	IDJAGRejectionInvalidRequest IDJAGRejectionReason = "invalid_request"
	// IDJAGRejectionResourceMismatch: a resource indicator named another server.
	IDJAGRejectionResourceMismatch IDJAGRejectionReason = "resource_mismatch"
	// IDJAGRejectionSubjectNotAuthorized: the resolved user lacks access.
	IDJAGRejectionSubjectNotAuthorized IDJAGRejectionReason = "subject_not_authorized"
	// IDJAGRejectionResourceNotAvailable: the assertion named no resource and
	// the resolved user lacks access to the server the request chose.
	IDJAGRejectionResourceNotAvailable IDJAGRejectionReason = "resource_not_available"
	// IDJAGRejectionUpstreamCredentialChoice: the requested upstream
	// credential choice was refused.
	IDJAGRejectionUpstreamCredentialChoice IDJAGRejectionReason = "upstream_credential_choice"
	// IDJAGRejectionAssertionOther: the validator refused the assertion for
	// a reason this set does not name yet.
	IDJAGRejectionAssertionOther IDJAGRejectionReason = "assertion_other"
)

// idJAGValidatorReasons are the validator reasons recorded as themselves.
var idJAGValidatorReasons = map[idjag.Reason]struct{}{
	idjag.ReasonMalformed:              {},
	idjag.ReasonTypeMismatch:           {},
	idjag.ReasonTrustUnavailable:       {},
	idjag.ReasonIssuerMismatch:         {},
	idjag.ReasonKeyUnknown:             {},
	idjag.ReasonKeyUnresolvable:        {},
	idjag.ReasonSignatureInvalid:       {},
	idjag.ReasonClaimsMalformed:        {},
	idjag.ReasonAudienceMismatch:       {},
	idjag.ReasonResourceMismatch:       {},
	idjag.ReasonResourceNotIndicated:   {},
	idjag.ReasonClientMismatch:         {},
	idjag.ReasonSubjectMissing:         {},
	idjag.ReasonEmailMissing:           {},
	idjag.ReasonExpiryMissing:          {},
	idjag.ReasonIssuedAtMissing:        {},
	idjag.ReasonExpired:                {},
	idjag.ReasonNotYetValid:            {},
	idjag.ReasonLifetimeTooLong:        {},
	idjag.ReasonIDMissing:              {},
	idjag.ReasonNotProvisioned:         {},
	idjag.ReasonSubjectUnavailable:     {},
	idjag.ReasonReplayed:               {},
	idjag.ReasonReplayStoreUnavailable: {},
	idjag.ReasonVerifierMisconfigured:  {},
}

// IDJAGRejectionFromValidator maps a validator reason into the closed set.
// A reason added to the validator later records as assertion_other until it
// is listed here.
func IDJAGRejectionFromValidator(reason idjag.Reason) IDJAGRejectionReason {
	if _, ok := idJAGValidatorReasons[reason]; ok {
		return IDJAGRejectionReason(reason)
	}
	return IDJAGRejectionAssertionOther
}

// RecordIDJAGExchange records one ID-JAG token exchange. reason is set only
// for a rejection. The client ID is deliberately not a dimension: it may be a
// client metadata document URL chosen by the caller.
func (m *Metrics) RecordIDJAGExchange(ctx context.Context, issuerID string, outcome IDJAGExchangeOutcome, reason IDJAGRejectionReason) {
	if m == nil || m.oauthIDJAGExchangeCounter == nil {
		return
	}
	kv := []attribute.KeyValue{
		attr.UserSessionIssuerID(issuerID),
		attr.OAuthIDJAGOutcome(string(outcome)),
	}
	if reason != IDJAGRejectionNone {
		kv = append(kv, attr.OAuthIDJAGRejectionReason(reason))
	}
	m.oauthIDJAGExchangeCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}
