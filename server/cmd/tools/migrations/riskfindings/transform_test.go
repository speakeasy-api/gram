package riskfindings

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func testFingerprinter(t *testing.T) risk.Fingerprinter {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32))
	keyring := fmt.Sprintf(`{"current":"v1","keys":{"v1":%q}}`, key)
	fp, err := risk.ParsePepperKeyRing([]byte(keyring))
	require.NoError(t, err)
	return fp
}

func TestTransformComputesFingerprintsAndRedaction(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID:                uuid.New(),
		CreatedAt:         time.Now().UTC(),
		OrganizationID:    "org_123",
		ProjectID:         uuid.New(),
		RiskPolicyID:      uuid.New(),
		RiskPolicyVersion: 7,
		ChatMessageID:     uuid.New(),
		Source:            "presidio",
		Found:             true,
		RuleID:            conv.PtrEmpty("pii.email_address"),
		Description:       conv.PtrEmpty("email"),
		Match:             conv.PtrEmpty("alice@example.com"),
		StartPos:          nil,
		EndPos:            nil,
		Confidence:        nil,
		Tags:              []string{"pii"},
		DeadLetterReason:  nil,
		ExcludedAt:        nil,
		ExclusionID:       nil,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	row := out[0]

	require.Equal(t, in.ID, row.ID)
	require.Equal(t, in.ProjectID.String(), row.ProjectID)
	require.Equal(t, in.ChatMessageID.String(), row.ChatMessageID)
	require.Equal(t, "pii.email_address", row.RuleID)
	require.Equal(t, []string{"pii"}, row.Tags)
	require.Empty(t, row.RequestID)

	require.NotEmpty(t, row.FingerprintGlobalHS256)
	require.NotEmpty(t, row.FingerprintTenantHS256)
	require.Equal(t, "v1", row.FingerprintPepperVersion)
	require.NotEqual(t, row.FingerprintGlobalHS256, row.FingerprintTenantHS256)

	require.Equal(t, uint32(len("alice@example.com")), row.MatchLen)
	require.Contains(t, row.MatchRedacted, "<redacted len=17 sha=")
	require.NotContains(t, row.MatchRedacted, "alice@example.com")
}

func TestTransformIsDeterministic(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_abc",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), RiskPolicyVersion: 1,
		ChatMessageID: uuid.New(), Source: "gitleaks", Found: true, RuleID: conv.PtrEmpty("secret.token"),
		Match: conv.PtrEmpty("secret-token"), Tags: []string{"secret"},
	}

	first, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	second, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)

	require.Equal(t, first[0].FingerprintGlobalHS256, second[0].FingerprintGlobalHS256)
	require.Equal(t, first[0].FingerprintTenantHS256, second[0].FingerprintTenantHS256)
	require.Equal(t, first[0].MatchRedacted, second[0].MatchRedacted)
}

func TestTransformTenantFingerprintIsOrgScoped(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	base := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), ProjectID: uuid.New(),
		RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(), Source: "gitleaks",
		Found: true, RuleID: conv.PtrEmpty("secret.token"),
		Match: conv.PtrEmpty("same-secret"), Tags: []string{},
	}

	a := base
	a.OrganizationID = "org_a"
	b := base
	b.OrganizationID = "org_b"

	ra, err := tf.Transform(t.Context(), a)
	require.NoError(t, err)
	rb, err := tf.Transform(t.Context(), b)
	require.NoError(t, err)

	// Same secret, same global fingerprint; different org, different tenant one.
	require.Equal(t, ra[0].FingerprintGlobalHS256, rb[0].FingerprintGlobalHS256)
	require.NotEqual(t, ra[0].FingerprintTenantHS256, rb[0].FingerprintTenantHS256)
}

func TestTransformDropsDeadLetterSentinel(t *testing.T) {
	t.Parallel()

	// Dead-letter rows are found=false sentinels; the live path never emits them
	// to ClickHouse, so the transform drops them.
	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(),
		Source: "presidio", Found: false, DeadLetterReason: conv.PtrEmpty("could not analyze"),
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestTransformDropsNonFinding(t *testing.T) {
	t.Parallel()

	// found=true but no rule_id, and found=false with a rule_id, are both dropped.
	tf := NewTransformer(testFingerprinter(t))
	noRule := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(),
		Source: "none", Found: true, RuleID: nil,
	}
	notFound := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(),
		Source: "none", Found: false, RuleID: conv.PtrEmpty("pii.email_address"),
	}

	outNoRule, err := tf.Transform(t.Context(), noRule)
	require.NoError(t, err)
	require.Empty(t, outNoRule)

	outNotFound, err := tf.Transform(t.Context(), notFound)
	require.NoError(t, err)
	require.Empty(t, outNotFound)
}

func TestTransformMapsExclusion(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	excludedAt := time.Now().UTC()
	exclusionID := uuid.New()
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("x"), ExcludedAt: &excludedAt, ExclusionID: &exclusionID,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.NotNil(t, out[0].ExcludedAt)
	require.Equal(t, excludedAt, *out[0].ExcludedAt)
	require.NotNil(t, out[0].ExclusionID)
	require.Equal(t, exclusionID, *out[0].ExclusionID)
}

func TestTransformNilTagsBecomeEmptySlice(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: uuid.New(),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("x"), Tags: nil,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.NotNil(t, out[0].Tags)
	require.Empty(t, out[0].Tags)
}
