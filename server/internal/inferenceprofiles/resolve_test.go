package inferenceprofiles_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/inferenceprofiles"
)

var (
	bedrock = inferenceprofiles.Profile{
		ID: uuid.New(), Name: "Bedrock", Provider: inferenceprofiles.ProviderBedrock,
		Settings: inferenceprofiles.Settings{Region: "us-west-2", AWSProfile: "dev-bedrock", GCPProjectID: "", BaseURL: "", Models: inferenceprofiles.Models{Default: "", Opus: "", Sonnet: "", Haiku: ""}},
	}
	vertex = inferenceprofiles.Profile{
		ID: uuid.New(), Name: "Vertex", Provider: inferenceprofiles.ProviderVertex,
		Settings: inferenceprofiles.Settings{Region: "us-east5", AWSProfile: "", GCPProjectID: "acme-ai-prod", BaseURL: "", Models: inferenceprofiles.Models{Default: "", Opus: "", Sonnet: "", Haiku: ""}},
	}
	gateway = inferenceprofiles.Profile{
		ID: uuid.New(), Name: "Gateway", Provider: inferenceprofiles.ProviderGateway,
		Settings: inferenceprofiles.Settings{Region: "", AWSProfile: "", GCPProjectID: "", BaseURL: "https://llm.example.com/v1", Models: inferenceprofiles.Models{Default: "", Opus: "", Sonnet: "", Haiku: ""}},
	}
)

func candidate(profile inferenceprofiles.Profile, principal, harness string, position int) inferenceprofiles.Candidate {
	return inferenceprofiles.Candidate{
		Rule: inferenceprofiles.Rule{
			ID: uuid.New(), ProfileID: profile.ID, PrincipalURN: principal, Harness: harness, Position: position,
			CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		Profile: profile,
	}
}

func resolvedNames(t *testing.T, candidates ...inferenceprofiles.Candidate) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, resolution := range inferenceprofiles.Resolve(candidates) {
		if resolution.Matched {
			out[resolution.Harness] = resolution.Profile.Name
		} else {
			out[resolution.Harness] = ""
		}
	}
	return out
}

func TestResolve_Precedence(t *testing.T) {
	t.Parallel()

	role := "role:organization:" + uuid.NewString()

	tests := []struct {
		name       string
		candidates []inferenceprofiles.Candidate
		want       map[string]string
	}{
		{
			name:       "no rules leaves every harness unmatched",
			candidates: nil,
			want:       map[string]string{"claude_code": "", "codex": ""},
		},
		{
			name: "user beats role",
			candidates: []inferenceprofiles.Candidate{
				candidate(gateway, role, "", 0),
				candidate(bedrock, "user:user_1", "", 5),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": "Bedrock"},
		},
		{
			name: "email beats directory group",
			candidates: []inferenceprofiles.Candidate{
				candidate(gateway, "directory_group:"+uuid.NewString(), "", 0),
				candidate(bedrock, "email:dev@example.com", "", 0),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": "Bedrock"},
		},
		{
			name: "role beats user:all",
			candidates: []inferenceprofiles.Candidate{
				candidate(gateway, "user:all", "", 0),
				candidate(bedrock, role, "", 9),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": "Bedrock"},
		},
		{
			name: "harness-specific beats all-harness at the same level",
			candidates: []inferenceprofiles.Candidate{
				candidate(gateway, role, "", 0),
				candidate(bedrock, role+"x", "claude_code", 3),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": "Gateway"},
		},
		{
			name: "a more specific all-harness rule beats a less specific harness rule",
			candidates: []inferenceprofiles.Candidate{
				candidate(bedrock, "user:all", "claude_code", 0),
				candidate(gateway, role, "", 0),
			},
			want: map[string]string{"claude_code": "Gateway", "codex": "Gateway"},
		},
		{
			name: "lower position breaks a tie",
			candidates: []inferenceprofiles.Candidate{
				candidate(gateway, role, "", 2),
				candidate(bedrock, "role:organization:"+uuid.NewString(), "", 1),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": "Bedrock"},
		},
		{
			name: "codex skips an all-harness Vertex rule and falls through",
			candidates: []inferenceprofiles.Candidate{
				candidate(vertex, "user:user_1", "", 0),
				candidate(gateway, "user:all", "", 0),
			},
			want: map[string]string{"claude_code": "Vertex", "codex": "Gateway"},
		},
		{
			name: "a rule for one harness leaves the other unmatched",
			candidates: []inferenceprofiles.Candidate{
				candidate(bedrock, "user:all", "claude_code", 0),
			},
			want: map[string]string{"claude_code": "Bedrock", "codex": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolvedNames(t, tt.candidates...))
		})
	}
}

func TestResolve_OlderRuleBreaksPositionTie(t *testing.T) {
	t.Parallel()
	older := candidate(bedrock, "role:organization:"+uuid.NewString(), "", 0)
	newer := candidate(gateway, "role:organization:"+uuid.NewString(), "", 0)
	newer.Rule.CreatedAt = older.Rule.CreatedAt.Add(time.Minute)
	require.Equal(t, "Bedrock", resolvedNames(t, newer, older)["claude_code"])
}

func render(t *testing.T, harness string, profile inferenceprofiles.Profile) string {
	t.Helper()
	encoded, err := json.Marshal(inferenceprofiles.Render(harness, profile))
	require.NoError(t, err)
	return string(encoded)
}

func TestRender(t *testing.T) {
	t.Parallel()

	pinned := bedrock
	pinned.Settings.BaseURL = "https://bedrock-runtime.vpce.example.com"
	pinned.Settings.Models = inferenceprofiles.Models{
		Default: "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Opus:    "us.anthropic.claude-opus-4-1-20250805-v1:0",
		Sonnet:  "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Haiku:   "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	}
	noProfile := bedrock
	noProfile.Settings.AWSProfile = ""
	gatewayWithModel := gateway
	gatewayWithModel.Settings.Models.Default = "gpt-5.5"

	tests := []struct {
		name    string
		harness string
		profile inferenceprofiles.Profile
		want    string
	}{
		{"claude code bedrock", "claude_code", bedrock,
			`{"awsAuthRefresh":"aws sso login --profile dev-bedrock","env":{"AWS_PROFILE":"dev-bedrock","AWS_REGION":"us-west-2","CLAUDE_CODE_USE_BEDROCK":"1"}}`},
		{"claude code bedrock without profile has no refresh", "claude_code", noProfile,
			`{"env":{"AWS_REGION":"us-west-2","CLAUDE_CODE_USE_BEDROCK":"1"}}`},
		{"claude code bedrock with base url and pins", "claude_code", pinned,
			`{"awsAuthRefresh":"aws sso login --profile dev-bedrock","env":{"ANTHROPIC_BEDROCK_BASE_URL":"https://bedrock-runtime.vpce.example.com","ANTHROPIC_DEFAULT_HAIKU_MODEL":"us.anthropic.claude-haiku-4-5-20251001-v1:0","ANTHROPIC_DEFAULT_OPUS_MODEL":"us.anthropic.claude-opus-4-1-20250805-v1:0","ANTHROPIC_DEFAULT_SONNET_MODEL":"us.anthropic.claude-sonnet-4-5-20250929-v1:0","ANTHROPIC_MODEL":"us.anthropic.claude-sonnet-4-5-20250929-v1:0","AWS_PROFILE":"dev-bedrock","AWS_REGION":"us-west-2","CLAUDE_CODE_USE_BEDROCK":"1"}}`},
		{"claude code vertex", "claude_code", vertex,
			`{"env":{"ANTHROPIC_VERTEX_PROJECT_ID":"acme-ai-prod","CLAUDE_CODE_USE_VERTEX":"1","CLOUD_ML_REGION":"us-east5"},"gcpAuthRefresh":"gcloud auth application-default login"}`},
		{"claude code gateway", "claude_code", gateway,
			`{"env":{"ANTHROPIC_BASE_URL":"https://llm.example.com/v1"}}`},
		{"codex bedrock", "codex", pinned,
			`{"model":"us.anthropic.claude-sonnet-4-5-20250929-v1:0","model_provider":"amazon-bedrock","model_providers":{"amazon-bedrock":{"aws":{"profile":"dev-bedrock","region":"us-west-2"}}}}`},
		{"codex gateway", "codex", gatewayWithModel,
			`{"model":"gpt-5.5","model_provider":"speakeasy","model_providers":{"speakeasy":{"base_url":"https://llm.example.com/v1","name":"Speakeasy gateway","wire_api":"responses"}}}`},
		{"codex vertex renders nothing", "codex", vertex, `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.JSONEq(t, tt.want, render(t, tt.harness, tt.profile))
		})
	}
}

func TestToolConfig_OmitsUnmatchedHarnesses(t *testing.T) {
	t.Parallel()
	config := inferenceprofiles.ToolConfig(inferenceprofiles.Resolve([]inferenceprofiles.Candidate{
		candidate(vertex, "user:all", "", 0),
	}))
	require.Contains(t, config, "claude_code")
	require.NotContains(t, config, "codex")
	require.Empty(t, inferenceprofiles.ToolConfig(inferenceprofiles.Resolve(nil)))
}

func TestValidateProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		settings inferenceprofiles.Settings
		wantErr  string
	}{
		{"bedrock ok", "bedrock", bedrock.Settings, ""},
		{"vertex ok", "vertex", vertex.Settings, ""},
		{"gateway ok", "gateway", gateway.Settings, ""},
		{"govcloud region ok", "bedrock", inferenceprofiles.Settings{Region: "us-gov-west-1"}, ""},
		{"vertex global ok", "vertex", inferenceprofiles.Settings{Region: "global", GCPProjectID: "acme-ai-prod"}, ""},
		{"bedrock arn model ok", "bedrock", inferenceprofiles.Settings{Region: "us-east-1", Models: inferenceprofiles.Models{Default: "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc123"}}, ""},
		{"vertex model ok", "vertex", inferenceprofiles.Settings{Region: "us-east5", GCPProjectID: "acme-ai-prod", Models: inferenceprofiles.Models{Sonnet: "claude-sonnet-4-5@20250929"}}, ""},
		{"unknown provider", "azure", inferenceprofiles.Settings{}, "provider must be"},
		{"bedrock needs region", "bedrock", inferenceprofiles.Settings{}, "region is required"},
		{"vertex needs project", "vertex", inferenceprofiles.Settings{Region: "us-east5"}, "gcp_project_id is required"},
		{"gateway needs base url", "gateway", inferenceprofiles.Settings{}, "base_url is required"},
		{"gateway rejects region", "gateway", inferenceprofiles.Settings{BaseURL: "https://llm.example.com", Region: "us-west-2"}, "region does not apply"},
		{"vertex rejects aws profile", "vertex", inferenceprofiles.Settings{Region: "us-east5", GCPProjectID: "acme-ai-prod", AWSProfile: "x"}, "aws_profile does not apply"},
		{"bad region", "bedrock", inferenceprofiles.Settings{Region: "US West 2"}, "region must be"},
		{"region injection", "bedrock", inferenceprofiles.Settings{Region: "us-west-2;rm"}, "region must be"},
		{"aws profile shell metacharacters", "bedrock", inferenceprofiles.Settings{Region: "us-west-2", AWSProfile: "dev; curl evil"}, "aws_profile may contain"},
		{"bad project id", "vertex", inferenceprofiles.Settings{Region: "us-east5", GCPProjectID: "Acme_AI"}, "gcp_project_id must be"},
		{"http base url", "gateway", inferenceprofiles.Settings{BaseURL: "http://llm.example.com"}, "base_url must be an https URL"},
		{"base url with credentials", "gateway", inferenceprofiles.Settings{BaseURL: "https://user:secret@llm.example.com"}, "must not carry credentials"},
		{"base url with query", "gateway", inferenceprofiles.Settings{BaseURL: "https://llm.example.com/?key=secret"}, "must not carry a query"},
		{"relative base url", "gateway", inferenceprofiles.Settings{BaseURL: "llm.example.com"}, "base_url must be an https URL"},
		{"bad model", "bedrock", inferenceprofiles.Settings{Region: "us-west-2", Models: inferenceprofiles.Models{Haiku: "claude haiku"}}, "models.haiku is not a valid model ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := inferenceprofiles.ValidateProfile(tt.provider, tt.settings)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateHarness(t *testing.T) {
	t.Parallel()
	require.NoError(t, inferenceprofiles.ValidateHarness("", "vertex"))
	require.NoError(t, inferenceprofiles.ValidateHarness("claude_code", "vertex"))
	require.NoError(t, inferenceprofiles.ValidateHarness("codex", "bedrock"))
	require.ErrorContains(t, inferenceprofiles.ValidateHarness("codex", "vertex"), "Codex cannot use a vertex profile")
	require.ErrorContains(t, inferenceprofiles.ValidateHarness("cursor", "bedrock"), "harness must be")
}
