package mcpregistry

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

func oktaRecord(name string, names ...string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, strconv.Quote(n))
	}
	raw := strings.Replace(basicRecord, "example.test/demo", name, 1)
	return strings.TrimSuffix(raw, "}") + `,"_meta":{"com.speakeasy.ai/okta":{"oinNames":[` + strings.Join(quoted, ",") + `],"oinIntegrationId":"4080826","xaaSignOnModes":["SAML_2_0"],"xaaIssuer":"https://auth.example.test"}}}`
}

func TestParseOktaMapping(t *testing.T) {
	t.Parallel()
	mapping, err := ParseOktaMapping(json.RawMessage(oktaRecord("example.test/linear", "integrator-4080826_linear_1", "linear")))
	require.NoError(t, err)
	require.Equal(t, OktaMapping{
		OINNames:         []string{"integrator-4080826_linear_1", "linear"},
		OINIntegrationID: "4080826",
		XAASignOnModes:   []string{"SAML_2_0"},
		XAAIssuer:        "https://auth.example.test",
	}, mapping)
	_, err = ParseOktaMapping(json.RawMessage(basicRecord))
	require.ErrorIs(t, err, ErrNoOktaMapping)
	for _, raw := range []string{`{"_meta":{"com.speakeasy.ai/okta":"nope"}}`, `{"_meta":{"com.speakeasy.ai/okta":null}}`} {
		_, err = ParseOktaMapping(json.RawMessage(raw))
		require.Error(t, err, raw)
		require.NotErrorIs(t, err, ErrNoOktaMapping, raw)
	}
	// Case-variant and unknown fields inside the namespace never decode.
	for _, raw := range []string{
		`{"_meta":{"com.speakeasy.ai/okta":{"OINNames":["linear"]}}}`,
		`{"_meta":{"com.speakeasy.ai/okta":{"oinnames":["linear"]}}}`,
		`{"_meta":{"com.speakeasy.ai/okta":{"oinNames":["linear"],"XAAISSUER":"https://auth.example.test"}}}`,
		`{"_meta":{"com.speakeasy.ai/okta":{"oinNames":["linear"],"unknown":true}}}`,
	} {
		mapping, err = ParseOktaMapping(json.RawMessage(raw))
		require.Error(t, err, raw)
		require.NotErrorIs(t, err, ErrNoOktaMapping, raw)
		require.Empty(t, mapping, raw)
	}
	// A case-variant root key is not the namespace, matching the SQL scan.
	_, err = ParseOktaMapping(json.RawMessage(strings.Replace(oktaRecord("example.test/linear", "linear"), `"_meta"`, `"_Meta"`, 1)))
	require.ErrorIs(t, err, ErrNoOktaMapping)
}

func TestOktaMappingValidation(t *testing.T) {
	t.Parallel()
	v, err := LoadValidator()
	require.NoError(t, err)
	for _, tc := range []struct {
		issuer string
		valid  bool
	}{
		{"https://auth.example.test", true},
		{"https://auth.example.test/oauth", true},
		{"https://auth.example.test:8443/", true},
		{"https://auth.example.test?x=1", false},
		{"https://auth.example.test/#frag", false},
		{"https://user@auth.example.test", false},
		{"http://auth.example.test", false},
		{"HTTPS://auth.example.test", false},
		{"https:///missing-host", false},
		{"https://auth.example.test/%zz", false},
		{"https://auth.example.test\u00a0", false},
		{"not a url", false},
	} {
		raw := strings.Replace(oktaRecord("example.test/demo", "linear"), `"https://auth.example.test"`, strconv.Quote(tc.issuer), 1)
		issues := v.Validate([]byte(raw))
		require.Equal(t, tc.valid, len(issues) == 0, "%s: %v", tc.issuer, issues)
		if !tc.valid {
			require.True(t, strings.HasPrefix(issues[0].Path, "/_meta/com.speakeasy.ai~1okta/xaaIssuer"), issues[0].Path)
		}
	}
	for _, name := range []string{"linear\u00a0", "lin\u200bear", "linear\t"} {
		issues := v.Validate([]byte(oktaRecord("example.test/demo", name)))
		require.NotEmpty(t, issues, name)
		require.Equal(t, "/_meta/com.speakeasy.ai~1okta/oinNames/0", issues[0].Path)
	}
	require.Empty(t, v.Validate([]byte(oktaRecord("example.test/demo", "linéar"))))
}

func TestOktaMappingUniqueness(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	linear, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/linear", "integrator-4080826_linear_1", "linear")))
	require.NoError(t, err)

	// Another entry cannot claim a mapped key, in any position.
	_, err = s.Create(ctx, json.RawMessage(oktaRecord("example.test/other", "other", "linear")))
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, []Issue{{Path: "/_meta/com.speakeasy.ai~1okta/oinNames/1", Message: "OIN name is already mapped by entry example.test/linear"}}, invalid.Issues)
	_, err = s.GetByName(ctx, "example.test/other")
	require.ErrorIs(t, err, ErrNotFound)

	// Unmapped keys are free; an entry re-saving its own keys is not a conflict.
	other, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/other", "other")))
	require.NoError(t, err)
	linear, err = s.Save(ctx, linear.ID, Token(linear), linear.Data)
	require.NoError(t, err)
	_, err = s.Save(ctx, other.ID, Token(other), json.RawMessage(oktaRecord("example.test/other", "other", "integrator-4080826_linear_1")))
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "/_meta/com.speakeasy.ai~1okta/oinNames/1", invalid.Issues[0].Path)

	// Unpublished entries keep their claim; dropping the namespace releases it.
	linear, err = s.SetPublished(ctx, linear.ID, Token(linear), false)
	require.NoError(t, err)
	_, err = s.Save(ctx, other.ID, Token(other), json.RawMessage(oktaRecord("example.test/other", "linear")))
	require.ErrorAs(t, err, &invalid)
	_, err = s.Save(ctx, linear.ID, Token(linear), json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/linear", 1)))
	require.NoError(t, err)
	other, err = s.Save(ctx, other.ID, Token(other), json.RawMessage(oktaRecord("example.test/other", "linear")))
	require.NoError(t, err)
	mapping, err := ParseOktaMapping(other.Data)
	require.NoError(t, err)
	require.Equal(t, []string{"linear"}, mapping.OINNames)

	// Invalid historical rows never block a claim: a non-array namespace, a
	// case-variant root key, and non-string elements all read as unmapped.
	for i, raw := range []string{
		`{"server":{"name":"example.test/broken-%d","description":"Broken","version":"1"},"_meta":{"com.speakeasy.ai/okta":{"oinNames":"notion"}}}`,
		`{"server":{"name":"example.test/broken-%d","description":"Broken","version":"1"},"_Meta":{"com.speakeasy.ai/okta":{"oinNames":["notion"]}}}`,
		`{"server":{"name":"example.test/broken-%d","description":"Broken","version":"1"},"_meta":{"com.speakeasy.ai/okta":{"oinNames":[null,5,["notion"],{"notion":1}]}}}`,
		`{"server":{"name":"example.test/broken-%d","description":"Broken","version":"1"},"_meta":{"com.speakeasy.ai/okta":null}}`,
	} {
		id := uuid.New()
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(strings.Replace(raw, "%d", strconv.Itoa(i), 1)), Published: true}))
	}
	_, err = s.Create(ctx, json.RawMessage(oktaRecord("example.test/notion", "notion")))
	require.NoError(t, err)
	// A numeric 5 in a historical array never blocks the string "5".
	_, err = s.Create(ctx, json.RawMessage(oktaRecord("example.test/five", "5")))
	require.NoError(t, err)
}

func TestOktaMappingPublishConflict(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	_, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/holder", "shared")))
	require.NoError(t, err)

	// Before this namespace had validation, retained records could contain
	// duplicate claims. Publishing must not make those mappings discoverable.
	id := uuid.New()
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{
		ID: id, Data: []byte(oktaRecord("example.test/retained", "shared")), Published: true,
	}))
	retained, err := s.Get(ctx, id)
	require.NoError(t, err)
	// Unpublishing must remain available even when the mapping conflicts.
	retained, err = s.SetPublished(ctx, id, Token(retained), false)
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, id, Token(retained), true)
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, []Issue{{Path: oktaNamespacePath + "/oinNames/0", Message: "OIN name is already mapped by entry example.test/holder"}}, invalid.Issues)
	retained, err = s.Get(ctx, id)
	require.NoError(t, err)
	require.False(t, retained.Published)

	// A repaired claim can be published normally.
	retained, err = s.Save(ctx, id, Token(retained), json.RawMessage(oktaRecord("example.test/retained", "unique")))
	require.NoError(t, err)
	retained, err = s.SetPublished(ctx, id, Token(retained), true)
	require.NoError(t, err)
	require.True(t, retained.Published)
}

func TestOktaMappingConcurrentClaims(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"create-create", "create-save", "save-save"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			e, err := s.Create(ctx, json.RawMessage(basicRecord))
			require.NoError(t, err)
			// save-save: the holder re-saves its claim while another entry
			// tries to take it; row locks precede the advisory lock on both.
			holder, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/holder", "held")))
			require.NoError(t, err)
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := range 2 {
				go func() {
					<-start
					var err error
					switch {
					case mode == "save-save" && i == 0:
						_, err = s.Save(ctx, holder.ID, Token(holder), holder.Data)
					case mode == "save-save":
						_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(oktaRecord("example.test/demo", "held")))
					case mode == "create-save" && i == 1:
						_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(oktaRecord("example.test/demo", "linear")))
					default:
						_, err = s.Create(ctx, json.RawMessage(oktaRecord("example.test/race-"+strconv.Itoa(i), "linear")))
					}
					results <- err
				}()
			}
			close(start)
			a, b := <-results, <-results
			if a != nil {
				a, b = b, a
			}
			require.NoError(t, a)
			var invalid *InvalidError
			require.ErrorAs(t, b, &invalid)
		})
	}
}
