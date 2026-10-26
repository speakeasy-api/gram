package chat

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func (s *Service) TestingCompletionTokens() *assistanttokens.Manager {
	s.authz = authz.NewEngine(s.logger, s.db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential})
	s.auth = auth.New(s.logger, s.db, s.sessions, s.authz)
	s.assistantTokens = assistanttokens.New("test-secret", s.db, nil, nil, nil)
	s.chatSessions = chatsessions.NewManager(s.logger, nil, "test-secret")
	return s.assistantTokens
}

func (s *Service) TestingAuthorizeCompletion(r *http.Request, completion bool) (context.Context, *contextvalues.AuthContext, billing.ModelUsageSource, error) {
	if completion {
		return s.authorizeCompletion(r)
	}
	return s.directAuthorize(r.Context(), r)
}
