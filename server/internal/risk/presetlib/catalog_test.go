package presetlib

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests exercise the engine directly (compileRule + compiledRule) rather
// than the embedded catalog, so they are independent of Track B's data edits.
// The one exception is the email matcher, which delegates to the presidiofp
// package's real placeholder catalog.

// ---------------------------------------------------------------------------
// luhnValid
// ---------------------------------------------------------------------------

func TestLuhnValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		digits string
		want   bool
	}{
		{"empty is not valid", "", false},
		{"visa test card", "4111111111111111", true},
		{"stripe test card", "4242424242424242", true},
		{"mastercard test card", "5555555555554444", true},
		{"amex test card (15 digits)", "378282246310005", true},
		{"off-by-one last digit fails", "4111111111111112", false},
		{"off-by-one interior digit fails", "4111111111111121", false},
		{"single digit zero is valid (0 mod 10)", "0", true},
		{"single digit five is not valid", "5", false},
		{"classic luhn example 79927398713", "79927398713", true},
		{"classic luhn example off by one", "79927398710", false},
		{"all zeros valid", "0000", true},
	}
	for _, tt := range tests {
		require.Equalf(t, tt.want, luhnValid(tt.digits), "luhnValid(%q)", tt.digits)
	}
}

// ---------------------------------------------------------------------------
// stripNonDigits
// ---------------------------------------------------------------------------

func TestStripNonDigits(t *testing.T) {
	t.Parallel()

	require.Empty(t, stripNonDigits(""))
	require.Equal(t, "4111111111111111", stripNonDigits("4111 1111 1111 1111"))
	require.Equal(t, "4111111111111111", stripNonDigits("4111-1111-1111-1111"))
	require.Equal(t, "1234", stripNonDigits("card: 1234!!!"))
	require.Empty(t, stripNonDigits("no digits here"))
	require.Empty(t, stripNonDigits("٤٢")) // Arabic-Indic digits are not ASCII 0-9
}

// ---------------------------------------------------------------------------
// helpers: build & compile a rule for a test, failing on unexpected error.
// ---------------------------------------------------------------------------

func mustCompile(t *testing.T, r Rule) *compiledRule {
	t.Helper()
	cr, err := compileRule(r)
	require.NoErrorf(t, err, "compileRule(%q)", r.ID)
	return &cr
}

// baseRule returns a minimally-valid rule with the given matcher; scope axes
// are left empty ("any") unless the test overrides them.
func baseRule(id string, m Matcher) Rule {
	return Rule{
		ID:          id,
		Description: "test",
		Reason:      "test reason",
		Sources:     nil,
		RuleIDs:     nil,
		RuleIDGlobs: nil,
		Match:       m,
	}
}

// ---------------------------------------------------------------------------
// digits matcher
// ---------------------------------------------------------------------------

func TestDigitsMatcher_ValueSetMembership(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("d1", Matcher{
		Type: MatchDigits, Values: []string{"4111111111111111"},
	}))

	// Exact digit run, and the same run with separators, both normalize to a hit.
	require.True(t, cr.valueMatches("4111111111111111"))
	require.True(t, cr.valueMatches("4111 1111 1111 1111"))
	require.True(t, cr.valueMatches("4111-1111-1111-1111"))
	// A different (even if Luhn-valid) number is not in the value set and Luhn
	// is off, so it does not match.
	require.False(t, cr.valueMatches("4242424242424242"))
	require.False(t, cr.valueMatches(""))
}

func TestDigitsMatcher_LuhnPathBoundedByLen(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("d2", Matcher{
		Type: MatchDigits, Luhn: true, MinLen: 13, MaxLen: 19,
	}))

	// Luhn-valid 16-digit PAN inside [13,19] passes.
	require.True(t, cr.valueMatches("4111111111111111"))
	require.True(t, cr.valueMatches("4111 1111 1111 1111"))
	// Non-Luhn 16-digit does NOT pass.
	require.False(t, cr.valueMatches("4111111111111112"))
	// Luhn-valid but too short (below min_len): 8-digit Luhn number.
	require.False(t, cr.valueMatches("00000000")) // 8 digits, Luhn-valid, < 13
	// Luhn-valid but too long (above max_len): 20-digit all zeros.
	require.False(t, cr.valueMatches("00000000000000000000")) // 20 digits > 19
	// Boundary: exactly min_len (13) all zeros is Luhn-valid and in range.
	require.True(t, cr.valueMatches("0000000000000"))
	// Boundary: exactly max_len (19) all zeros is Luhn-valid and in range.
	require.True(t, cr.valueMatches("0000000000000000000"))
}

func TestDigitsMatcher_MaxLenZeroMeansNoUpperBound(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("d3", Matcher{
		Type: MatchDigits, Luhn: true, MinLen: 13, MaxLen: 0,
	}))

	// 16 digits: fine.
	require.True(t, cr.valueMatches("4111111111111111"))
	// 40 digits, Luhn-valid (all zeros): with no upper bound, still matches.
	require.True(t, cr.valueMatches(strings.Repeat("0", 40)))
	// Below min_len still rejected even with no upper bound.
	require.False(t, cr.valueMatches("0000000000")) // 10 digits < 13
}

func TestDigitsMatcher_ValuesAndLuhnCombined(t *testing.T) {
	t.Parallel()

	// An explicit value that is SHORTER than min_len should still match via the
	// value set even though it could never match the Luhn/length path.
	cr := mustCompile(t, baseRule("d4", Matcher{
		Type:   MatchDigits,
		Values: []string{"1234"},
		Luhn:   true,
		MinLen: 13,
		MaxLen: 19,
	}))
	require.True(t, cr.valueMatches("1234"))             // value-set hit
	require.True(t, cr.valueMatches("4111111111111111")) // luhn-path hit
	require.False(t, cr.valueMatches("9999"))            // neither
}

// ---------------------------------------------------------------------------
// regex matcher — documents SUBSTRING (unanchored) semantics.
// ---------------------------------------------------------------------------

func TestRegexMatcher_AnchoredPattern(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("r1", Matcher{
		Type: MatchRegex, Patterns: []string{`^AKIA[0-9A-Z]{12}EXAMPLE$`},
	}))

	// AKIA + 12 [0-9A-Z] + EXAMPLE.
	const key = "AKIAABCDEFGH1234EXAMPLE"
	require.True(t, cr.valueMatches(key))
	// Anchored pattern rejects leading/trailing noise.
	require.False(t, cr.valueMatches("prefix "+key))
	require.False(t, cr.valueMatches(key+" suffix"))
	require.False(t, cr.valueMatches("AKIAABCDEFGH1234NOTEXAMPLE"))
	// Wrong middle length (11) fails the anchored {12}.
	require.False(t, cr.valueMatches("AKIAABCDEFGH123EXAMPLE"))
}

// TestRegexMatcher_UnanchoredIsSubstring locks the documented behavior: an
// UNANCHORED pattern matches anywhere in the value (MatchString semantics), so
// surrounding text still hits. This is a deliberate engine property; catalog
// authors must anchor with ^...$ for whole-value matching.
func TestRegexMatcher_UnanchoredIsSubstring(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("r2", Matcher{
		Type: MatchRegex, Patterns: []string{`AKIA[0-9A-Z]{12}EXAMPLE`},
	}))

	const key = "AKIAABCDEFGH1234EXAMPLE"
	require.True(t, cr.valueMatches(key))
	// Substring: leading/trailing text does NOT prevent a match.
	require.True(t, cr.valueMatches("token="+key+";"))
	require.True(t, cr.valueMatches(key+" and more"))
	// Still requires the pattern to appear somewhere.
	require.False(t, cr.valueMatches("no key here"))
}

func TestRegexMatcher_MultiplePatternsAnyMatches(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("r3", Matcher{
		Type: MatchRegex, Patterns: []string{`^h1:[A-Za-z0-9+/=]{40,}$`, `^[0-9a-f]{40}$`},
	}))

	require.True(t, cr.valueMatches("da39a3ee5e6b4b0d3255bfef95601890afd80709")) // 40 hex
	require.True(t, cr.valueMatches("h1:"+strings.Repeat("A", 44)))
	require.False(t, cr.valueMatches("not a hash"))
}

// ---------------------------------------------------------------------------
// exact matcher, with and without case_insensitive
// ---------------------------------------------------------------------------

func TestExactMatcher_CaseSensitive(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("e1", Matcher{
		Type: MatchExact, Values: []string{"AKIAEXAMPLE"},
	}))

	require.True(t, cr.valueMatches("AKIAEXAMPLE"))
	require.False(t, cr.valueMatches("akiaexample"))
	require.False(t, cr.valueMatches("AKIAEXAMPLE ")) // exact, not prefix
	require.False(t, cr.valueMatches("xAKIAEXAMPLE"))
}

func TestExactMatcher_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("e2", Matcher{
		Type: MatchExact, CaseInsensitive: true, Values: []string{"AKIAExample"},
	}))

	require.True(t, cr.valueMatches("AKIAEXAMPLE"))
	require.True(t, cr.valueMatches("akiaexample"))
	require.True(t, cr.valueMatches("AkIaExAmPlE"))
	require.False(t, cr.valueMatches("akiaexample2"))
}

// ---------------------------------------------------------------------------
// prefix matcher, with and without case_insensitive
// ---------------------------------------------------------------------------

func TestPrefixMatcher_CaseSensitive(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("p1", Matcher{
		Type: MatchPrefix, Values: []string{"sk_test_", "rk_test_"},
	}))

	require.True(t, cr.valueMatches("sk_test_abc123"))
	require.True(t, cr.valueMatches("rk_test_xyz"))
	require.True(t, cr.valueMatches("sk_test_")) // prefix equal to value
	require.False(t, cr.valueMatches("sk_live_abc123"))
	require.False(t, cr.valueMatches("SK_TEST_abc"))   // case-sensitive
	require.False(t, cr.valueMatches("x sk_test_abc")) // must be a prefix, not substring
}

func TestPrefixMatcher_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("p2", Matcher{
		Type: MatchPrefix, CaseInsensitive: true, Values: []string{"SK_Test_"},
	}))

	require.True(t, cr.valueMatches("sk_test_abc"))
	require.True(t, cr.valueMatches("SK_TEST_abc"))
	require.False(t, cr.valueMatches("sk_live_abc"))
}

// ---------------------------------------------------------------------------
// email matcher — delegates to presidiofp's placeholder email catalog.
// ---------------------------------------------------------------------------

func TestEmailMatcher_DelegatesToPresidiofp(t *testing.T) {
	t.Parallel()

	cr := mustCompile(t, baseRule("em1", Matcher{Type: MatchEmail}))

	// Placeholder / reserved-domain addresses hit.
	require.True(t, cr.valueMatches("noreply@example.com"))
	require.True(t, cr.valueMatches("anyone@example.com"))
	require.True(t, cr.valueMatches("git@github.com"))
	// A normal-looking address does not.
	require.False(t, cr.valueMatches("ada@speakeasy.com"))
	require.False(t, cr.valueMatches(""))
}

// ---------------------------------------------------------------------------
// scope axes: sources / rule_ids / rule_id_globs
// ---------------------------------------------------------------------------

func TestScope_EmptyAxesMeanAny(t *testing.T) {
	t.Parallel()

	// No sources, rule_ids, or globs => matches regardless of source/rule id.
	cr := mustCompile(t, baseRule("s1", Matcher{Type: MatchExact, Values: []string{"v"}}))

	require.True(t, cr.matches(Match{Source: "anything", RuleID: "whatever", Value: "v"}))
	require.True(t, cr.matches(Match{Source: "", RuleID: "", Value: "v"}))
	// Value still has to match.
	require.False(t, cr.matches(Match{Source: "anything", RuleID: "whatever", Value: "nope"}))
}

func TestScope_SourceConstraint(t *testing.T) {
	t.Parallel()

	r := baseRule("s2", Matcher{Type: MatchExact, Values: []string{"v"}})
	r.Sources = []string{"gitleaks"}
	cr := mustCompile(t, r)

	require.True(t, cr.matches(Match{Source: "gitleaks", RuleID: "x", Value: "v"}))
	require.False(t, cr.matches(Match{Source: "presidio", RuleID: "x", Value: "v"}))
	require.False(t, cr.matches(Match{Source: "", RuleID: "x", Value: "v"}))
}

func TestScope_RuleIDConstraint(t *testing.T) {
	t.Parallel()

	r := baseRule("s3", Matcher{Type: MatchExact, Values: []string{"v"}})
	r.RuleIDs = []string{"pii.credit_card"}
	cr := mustCompile(t, r)

	require.True(t, cr.matches(Match{Source: "x", RuleID: "pii.credit_card", Value: "v"}))
	require.False(t, cr.matches(Match{Source: "x", RuleID: "pii.email_address", Value: "v"}))
}

func TestScope_GlobConstraint(t *testing.T) {
	t.Parallel()

	r := baseRule("s4", Matcher{Type: MatchExact, Values: []string{"v"}})
	r.RuleIDGlobs = []string{"secret.stripe_*"}
	cr := mustCompile(t, r)

	require.True(t, cr.matches(Match{Source: "x", RuleID: "secret.stripe_access_token", Value: "v"}))
	require.True(t, cr.matches(Match{Source: "x", RuleID: "secret.stripe_", Value: "v"})) // prefix boundary
	require.False(t, cr.matches(Match{Source: "x", RuleID: "secret.other", Value: "v"}))
	require.False(t, cr.matches(Match{Source: "x", RuleID: "secret.strip", Value: "v"})) // shorter than prefix
}

func TestScope_RuleIDsAndGlobsAreUnioned(t *testing.T) {
	t.Parallel()

	r := baseRule("s5", Matcher{Type: MatchExact, Values: []string{"v"}})
	r.RuleIDs = []string{"pii.credit_card"}
	r.RuleIDGlobs = []string{"secret.stripe_*"}
	cr := mustCompile(t, r)

	require.True(t, cr.matches(Match{Source: "x", RuleID: "pii.credit_card", Value: "v"}))            // exact
	require.True(t, cr.matches(Match{Source: "x", RuleID: "secret.stripe_access_token", Value: "v"})) // glob
	require.False(t, cr.matches(Match{Source: "x", RuleID: "pii.email_address", Value: "v"}))         // neither
}

func TestScope_AllAxesMustHold(t *testing.T) {
	t.Parallel()

	r := baseRule("s6", Matcher{Type: MatchPrefix, Values: []string{"sk_test_"}})
	r.Sources = []string{"gitleaks"}
	r.RuleIDGlobs = []string{"secret.stripe_*"}
	cr := mustCompile(t, r)

	good := Match{Source: "gitleaks", RuleID: "secret.stripe_access_token", Value: "sk_test_abc"}
	require.True(t, cr.matches(good))
	// Break each axis in turn.
	bad := good
	bad.Source = "presidio"
	require.False(t, cr.matches(bad))
	bad = good
	bad.RuleID = "secret.other"
	require.False(t, cr.matches(bad))
	bad = good
	bad.Value = "sk_live_abc"
	require.False(t, cr.matches(bad))
}

// ---------------------------------------------------------------------------
// first-match-wins ordering (exercised through the public Reason path via a
// small in-memory compile+scan mirror, since load() reads the embedded file).
// ---------------------------------------------------------------------------

// scanRules mirrors Reason's first-match-wins loop over an in-memory rule set,
// so ordering can be tested without touching the embedded catalog.
func scanRules(t *testing.T, rules []Rule, m Match) string {
	t.Helper()
	for _, r := range rules {
		cr := mustCompile(t, r)
		if cr.matches(m) {
			return cr.rule.Reason
		}
	}
	return ""
}

func TestFirstMatchWins(t *testing.T) {
	t.Parallel()

	first := baseRule("f1", Matcher{Type: MatchPrefix, Values: []string{"sk_"}})
	first.Reason = "first reason"
	second := baseRule("f2", Matcher{Type: MatchPrefix, Values: []string{"sk_test_"}})
	second.Reason = "second reason"

	m := Match{Source: "x", RuleID: "y", Value: "sk_test_abc"}
	// Both rules match; the earlier one wins.
	require.Equal(t, "first reason", scanRules(t, []Rule{first, second}, m))
	// Reversed order: the other wins.
	require.Equal(t, "second reason", scanRules(t, []Rule{second, first}, m))
}

// ---------------------------------------------------------------------------
// compileRule validation — the negative cases the integrity test relies on.
// ---------------------------------------------------------------------------

func TestCompileRule_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    Rule
		wantSub string
	}{
		{
			name:    "empty id",
			rule:    Rule{ID: "  ", Reason: "r", Match: Matcher{Type: MatchExact, Values: []string{"v"}}},
			wantSub: "empty id",
		},
		{
			name:    "empty reason",
			rule:    Rule{ID: "x", Reason: "  ", Match: Matcher{Type: MatchExact, Values: []string{"v"}}},
			wantSub: "empty reason",
		},
		{
			name:    "unknown matcher type",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: "bogus"}},
			wantSub: "unknown match type",
		},
		{
			name:    "empty matcher type is unknown",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: ""}},
			wantSub: "unknown match type",
		},
		{
			name:    "exact needs values",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchExact}},
			wantSub: "needs values",
		},
		{
			name:    "prefix needs values",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchPrefix}},
			wantSub: "needs values",
		},
		{
			name:    "exact empty value rejected",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchExact, Values: []string{""}}},
			wantSub: "empty value",
		},
		{
			name:    "prefix empty value rejected",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchPrefix, Values: []string{"ok", ""}}},
			wantSub: "empty value",
		},
		{
			name:    "regex needs patterns",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchRegex}},
			wantSub: "needs patterns",
		},
		{
			name:    "regex empty pattern rejected",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchRegex, Patterns: []string{""}}},
			wantSub: "empty pattern",
		},
		{
			name:    "uncompilable regex",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchRegex, Patterns: []string{"("}}},
			wantSub: "bad regex",
		},
		{
			name:    "digits needs values or luhn",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchDigits}},
			wantSub: "needs values or luhn",
		},
		{
			name:    "digits non-digit value rejected",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchDigits, Values: []string{"12ab"}}},
			wantSub: "digits-only",
		},
		{
			name:    "digits empty value rejected",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchDigits, Values: []string{""}}},
			wantSub: "digits-only",
		},
		{
			name:    "digits+luhn needs positive min_len",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchDigits, Luhn: true, MinLen: 0, MaxLen: 19}},
			wantSub: "min_len > 0",
		},
		{
			name:    "digits min_len exceeds max_len",
			rule:    Rule{ID: "x", Reason: "r", Match: Matcher{Type: MatchDigits, Luhn: true, MinLen: 20, MaxLen: 19}},
			wantSub: "exceeds max_len",
		},
		{
			name:    "empty rule_id_glob rejected",
			rule:    Rule{ID: "x", Reason: "r", RuleIDGlobs: []string{""}, Match: Matcher{Type: MatchEmail}},
			wantSub: "non-empty prefix",
		},
		{
			name:    "bare-star rule_id_glob rejected",
			rule:    Rule{ID: "x", Reason: "r", RuleIDGlobs: []string{"*"}, Match: Matcher{Type: MatchEmail}},
			wantSub: "non-empty prefix",
		},
		{
			name:    "rule_id_glob without trailing star rejected",
			rule:    Rule{ID: "x", Reason: "r", RuleIDGlobs: []string{"secret.stripe_"}, Match: Matcher{Type: MatchEmail}},
			wantSub: "non-empty prefix",
		},
	}
	for _, tt := range tests {
		_, err := compileRule(tt.rule)
		require.Errorf(t, err, "case %q: expected an error", tt.name)
		require.Containsf(t, err.Error(), tt.wantSub, "case %q: error %q", tt.name, err.Error())
	}
}

func TestCompileRule_ValidCases(t *testing.T) {
	t.Parallel()

	valid := []Rule{
		baseRule("v-exact", Matcher{Type: MatchExact, Values: []string{"a"}}),
		baseRule("v-prefix-ci", Matcher{Type: MatchPrefix, CaseInsensitive: true, Values: []string{"A"}}),
		baseRule("v-regex", Matcher{Type: MatchRegex, Patterns: []string{"^a$"}}),
		baseRule("v-digits-values", Matcher{Type: MatchDigits, Values: []string{"4111111111111111"}}),
		baseRule("v-digits-luhn", Matcher{Type: MatchDigits, Luhn: true, MinLen: 13, MaxLen: 19}),
		baseRule("v-digits-luhn-nomax", Matcher{Type: MatchDigits, Luhn: true, MinLen: 13, MaxLen: 0}),
		baseRule("v-email", Matcher{Type: MatchEmail}),
	}
	for _, r := range valid {
		_, err := compileRule(r)
		require.NoErrorf(t, err, "rule %q should compile", r.ID)
	}
}

// TestDuplicateIDError covers the sentinel used by load() when two rules share
// an id (compileRule itself does not dedupe — dedup lives in load()).
func TestDuplicateIDError(t *testing.T) {
	t.Parallel()

	err := &dupIDError{id: "test-credit-cards"}
	require.Contains(t, err.Error(), "duplicate rule id")
	require.Contains(t, err.Error(), "test-credit-cards")
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func TestToSet(t *testing.T) {
	t.Parallel()

	require.Nil(t, toSet(nil))
	require.Nil(t, toSet([]string{}))
	s := toSet([]string{"a", "b", "a"})
	require.Len(t, s, 2)
	_, ok := s["a"]
	require.True(t, ok)
}

func TestLowerAll(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"ab", "cd"}, lowerAll([]string{"AB", "Cd"}))
	require.Equal(t, []string{}, lowerAll([]string{}))
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()

	require.Nil(t, sortedKeys(nil))
	require.Nil(t, sortedKeys(map[string]struct{}{}))
	got := sortedKeys(map[string]struct{}{"c": {}, "a": {}, "b": {}})
	require.Equal(t, []string{"a", "b", "c"}, got)
}

// ---------------------------------------------------------------------------
// public API surface over the embedded catalog (integration-lite).
// ---------------------------------------------------------------------------

func TestPublicReason_NoOpConsistency(t *testing.T) {
	t.Parallel()

	// A guaranteed non-match returns "" (real finding).
	require.Empty(t, Reason(Match{Source: "nope", RuleID: "nope", Value: "definitely not benign"}))
	// Version is a stable 8-char hex prefix.
	require.Len(t, Version(), 8)
}

func TestPublicRuleIDAccessorsAreSortedAndCopied(t *testing.T) {
	t.Parallel()

	ids := RuleIDs()
	require.True(t, sortDetectSorted(ids), "RuleIDs must be sorted")
	// Returned slice is a copy: mutating it must not affect the next call.
	if len(ids) > 0 {
		ids[0] = "ZZZ_mutated"
		require.NotEqual(t, "ZZZ_mutated", RuleIDs()[0])
	}

	globs := RuleIDGlobs()
	require.True(t, sortDetectSorted(globs), "RuleIDGlobs must be sorted")
}

func sortDetectSorted(xs []string) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			return false
		}
	}
	return true
}
