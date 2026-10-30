package declarative

import (
	"github.com/speakeasy-api/gram/server/internal/authz"
	s "github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
)

// The relationship schema for reading session transcripts:
//
//   - the session's verified human author reads it;
//   - on a session with no human attribution at all, the current owner of the
//     agent that produced it reads it, so agent-only history follows a
//     transfer;
//   - a chat:read (or chat:write) reviewer grant, or root, reads every
//     session in the organization.
//
// Nothing else does. In particular agent:read, agent:write, agent:authorize
// and agent:transfer are out of schema, so managing an agent never grants its
// transcripts, and org:admin is not a relation here at all.
const (
	typeUser  = "user"
	typeAgent = "agent"
	typeChat  = "chat"
)

// ChatPermission is a permission declared on the chat type.
type ChatPermission string

const ChatRead ChatPermission = "read"

func chatTypes() []*s.Type {
	const (
		org     = typeOrganization
		project = typeProject
	)
	root := s.AncestorRel(org, "root")

	return []*s.Type{
		s.NewType(typeUser, s.Parent(org)),

		// An agent's owner attribute is set only while the owner is eligible:
		// current, not latched for reassignment, and the agent not deleted.
		s.NewType(typeAgent, s.Parent(org), s.Attr("owner", typeUser),
			s.Permission("owner", s.Is("owner")),
		),

		s.NewType(typeChat, s.Parent(project), s.StoredKind(authz.ResourceKindChat),
			s.Attr("author", typeUser),
			// A human identity the session carries that is not a member id,
			// such as an external user's email. It never grants; it only marks
			// the session as human-attributed.
			s.Attr("external_author", typeUser),
			s.Attr("agent", typeAgent),

			s.Grant("reader", s.StoredAs(string(authz.ScopeChatRead)), s.AnchoredAt(org),
				s.Describe("Read every session transcript in the organization")),
			s.Grant("writer", s.StoredAs(string(authz.ScopeChatWrite)), s.AnchoredAt(org),
				s.Describe("Rename, annotate and delete any session; implies reading")),

			s.Permission(string(ChatRead), s.Union(
				s.Is("author"),
				s.Unless("author", s.Unless("external_author", s.Via("agent", "owner"))),
				s.Rel("reader"),
				s.Rel("writer"),
				root,
			)),
		),
	}
}
