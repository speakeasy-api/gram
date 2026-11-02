package mcp

import (
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// servedCredential names the auth client a call is signed in upstream with.
// Only a personal connection has an owner: a shared client signs every caller
// in with the same credential, so it names nobody even when the permit was
// decided against one.
func servedCredential(clientID uuid.UUID, clientType, connectionOwner string) contextvalues.UpstreamCredential {
	credential := contextvalues.UpstreamCredential{AuthClientID: "", AuthClientType: clientType, ConnectionOwnerUserID: ""}
	if clientID != uuid.Nil {
		credential.AuthClientID = clientID.String()
	}
	if clientType == authClientTypeRemoteSession {
		credential.ConnectionOwnerUserID = connectionOwner
	}
	return credential
}

// servedCredential is the credential a member resolution serves a call with:
// a resolved client, or a named client that hands serving back to the
// upstream client choice. No-auth, gaps and refusals serve none.
func (r *authClientResolution) servedCredential() (contextvalues.UpstreamCredential, bool) {
	var none contextvalues.UpstreamCredential
	if r == nil || r.clientType == "" {
		return none, false
	}
	if r.outcome != authClientResolved && (r.outcome != authClientLegacy || r.clientID == uuid.Nil) {
		return none, false
	}
	return servedCredential(r.clientID, r.clientType, r.connectionOwner), true
}

// memberServedCredential is the credential a routed member call is signed in
// with: the member's resolved auth client when the credential's choices serve
// it, otherwise what an agent's member authorization recorded for the
// registration the routed token belongs to.
func memberServedCredential(gate metaGateContext, member metaMember, routed remotesessions.UpstreamToken) (contextvalues.UpstreamCredential, bool) {
	if gate.memberChoices != nil && member.backend == metaMemberBackendProxied && gate.memberChoices.serves(member) {
		return member.authClient.servedCredential()
	}
	// A header member sends no token; its credential is recorded under no
	// registration.
	registration := routed.RemoteSessionClientID
	if routed.Token == "" {
		registration = uuid.Nil
	}
	credential, ok := gate.servedCredentials[registration]
	if !ok || credential.AuthClientType == "" {
		return contextvalues.UpstreamCredential{AuthClientID: "", AuthClientType: "", ConnectionOwnerUserID: ""}, false
	}
	return credential, true
}

// recordServedCredential notes, during a member dial that collects them, the
// auth client a registration's agent credential was authorized for. A header
// member, which has no registration, records under uuid.Nil. A use that named
// no auth client type records nothing.
func (g *metaGateContext) recordServedCredential(registration uuid.UUID, credential contextvalues.UpstreamCredential) {
	if g.servedCredentials == nil || credential.AuthClientType == "" {
		return
	}
	g.servedCredentials[registration] = credential
}
