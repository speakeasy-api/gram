re: Commit 1:

We actually _do_ want to remove the old URN references. This situation is not in production so we can afford to change references aggressively. If you have a good reason for not changing this now, please do let me know.

re: commit 2:

nice.

re: commit 3:

won't steps two and 3 conflict with the current paths for protected resource metadata? If not, are we concerned that the way MCP clients construct their metadata endpoints will miss this routes? Or is there something you know that I don't? I seem to recall when I pointed MCP clients to a protected-resource object at a path that wasn't the root, it would prefix that path with .well-known/oauth-protected-resource resulting in 404s at 
.well-known/oauth-protected-resource/.well-known/oauth-protected-resource/<slug>

also concerned about path conflicts on 4 and 5? We could always add v2/ prefix if we need to avoid the conflicts

re: commit 4

oh yeah, for sure I don't like this. We should just add new handlers. Not attempts to use the old handlers at all. We're losing the linearity of our nice authnchallenge.go file to bounce to these silly pre-existing handlers.

re: commit 5

nice

re: commit 6

> 4. Mint AuthnChallengeState Redis doc (per spike §4.3) with client_id, redirect_uri, code_challenge, original state, scope, user_session_issuer_id, plus mcp_session_id from header. Sub left empty here — HandleIDPCallback stamps it on the private path; HandleConsent stamps it on the public path. 

will we have an mcp_session_id header in this situation? we haven't successfully initialized yet, right? This strikes me as a problem. We might need to go the other way: generate a new UUID and use the anonymous sub id as the mcp session ID

Unresolved questions:

1) Yeah flip as mentioned earlier
2) Hmmmmmmm - I guess we can duplicate the manager, but let's add a comment to tops of both chatsessions/manager and our usersessions/jwt.go that we wish to remove chatsessions and replace them with usersessions entirely
3) We def want to land HandleRegister here
4) Both seems dead easy. let's do both
5) Former better for sure
6) Classy no need to rush into it, eh?
7) Set NULL is fine
8) Let's keep em granular and review at each step. Can always squash later ya know.
9) Let's dig in a bit here. how substantial would a re-implementation be? Frankly, I prefer scenarios wherein we make it as easy as possible to remove legacy codepaths - we don't want to write to the same session store so it seems to me like we can assemble our own login challenge fairly easily.

