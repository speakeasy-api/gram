package authz

import "github.com/google/uuid"

// Chat has no project dimension, and every chat grant the dashboard saves
// covers every chat (resource_id "*"). So the id in a chat check is a
// placeholder: Chats(project), OrgChats(org) and UnrestrictedChats() all ask
// whether the caller holds chat:read on every chat, and differ only in the
// placeholder they carry. They keep the ids the old ChatReadCheck callers
// passed, so a grant saved through the API on one specific id still matches
// exactly what it matched before. Giving chats a project boundary is a
// separate change.

// OrgChatCollection names every chat of one organization. Chats sit in
// projects, but an organization-wide chat read grant is stored with the
// organization's id as the resource, so reading across every project asks
// for that.
type OrgChatCollection struct{ org OrgID }

// OrgChats names every chat of org, across all of its projects.
func OrgChats(org OrgID) OrgChatCollection {
	return OrgChatCollection{org: org}
}

// Read requests ScopeChatRead on every chat of the organization.
func (c OrgChatCollection) Read() Check {
	return Chat(ChatID{id: c.org.id}).Read()
}

// UnrestrictedChatCollection names chats without bounding them to an
// organization, a project or a chat. Only an unrestricted chat grant covers
// it.
type UnrestrictedChatCollection struct{}

// UnrestrictedChats names every chat, for a request that only an
// unrestricted chat grant may make, such as revealing a payload whose chat is
// not recorded.
func UnrestrictedChats() UnrestrictedChatCollection {
	return UnrestrictedChatCollection{}
}

// Read requests ScopeChatRead on a resource no bounded grant names: the nil
// id, which only the wildcard matches.
func (UnrestrictedChatCollection) Read() Check {
	return Chat(ChatID{id: Some(uuid.Nil.String())}).Read()
}
