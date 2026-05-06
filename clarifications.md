# Prompt Clarifications

Answer inline under each question. Free-form prose is fine — terse is better than complete.

---

## A. Trailing-off sentences

### A1. prompt.md line 4

> "Currently, Gram has its OAuth server connect to remote servers."

How does this sentence end? My read: the same Gram component acts as both AS to the MCP client _and_ as a client to the remote, and these need to be split. Confirm or correct.

**Answer:**
Correct

---

### A2. prompt.md line 6

> "storing oauth secrets on an oauth proxy provider record, "

What other deprecated functionality belongs in this list? Candidates I see in the schema:

- `oauth_proxy_providers.secrets JSONB`
- `oauth_proxy_providers.security_key_names`
- `oauth_proxy_providers.provider_type = 'custom'`
- something else?

**Answer:**
those; certainly more; we'll have to figure them out as we design I think

---

### A3. prompt.md line 7

> "There should be logic in our authorize endpoint whenever a new scope exists on the oauth request."

What should that logic _do_? Proposed: if requested scope ⊄ previously-consented scope, force re-consent. Confirm.

**Answer:**
This was just absently written. It's not about whether a new scope exists. It's logic to see whether consent has been given from a given user to a given client_session_issuer to access _all_ of the remote_session_tokens

---

### A4. prompt.md line 14

> "We should conform to an oidc scope."

Which scope, and conform how? Options:

- (a) support `openid` scope on our AS and emit OIDC-shaped tokens
- (b) anonymous principal tokens still carry OIDC-shape claims (`sub`, `aud`, etc.) even without the scope
- (c) something else

**Answer:**
sorry - I meant to say oidc schema - not scope. We should have our jwt schema match oidc as you more or less suggest. notably, we aren't gonna switch to a public key signing algorithm as mandated by oidc but rather just stick with our current hum drum symmetric algo

---

### A5. prompt.md line 58 (milestone #0b)

> "static page powered by \_\_\_ and resources and allow for much more sophisticated functionality"

What renders the page? Options:

- (a) templ
- (b) embedded HTML + MCP resources
- (c) a tiny Vite app
- (d) something else

**Answer:**
I guess maybe we'll make a new project directory called mock-idp-ui with a little hono package that uses the hono client-side react package

---

### A6. prompt.md line 67 (milestone #6)

> "migrate all servers on current external_oauth_provider model to client_session_issuer with \_\_\_"

With what? Options:

- (a) with passthrough mode
- (b) with auto-discovered remote_oauth_issuer linkage
- (c) something else

**Answer:**
(a) passthrough mode

---

## B. Renumbering / naming

### B7. Two `5`s in goals (lines 8 and 10) and two `milestone #5`s (lines 66 and 67)

Want me to renumber? Proposed: goal-line-8 stays as 5, goal-line-10 → 6, shift remaining down. Milestones similarly fixed.

**Answer:**
yeah fix those puppies please.

---

### B8. `client_session_issuer` vs `client_sessions_issuer`

Line 44 uses both. Standardize on `client_session_issuer` (singular)?

**Answer:**
yes

---

### B9. `project.md` (line 45) vs `tickets.md` (line 91)

File on disk is `tickets.md`. Which name wins?

**Answer:**
project.md

---

### B10. Goal #7: "Remote OAuth providers should probably have OIDC"

Required, optional, or auto-detected?

**Answer:**
its required but defaults to false - we can enable it and may enable new behaviors

---

## C. Concept definitions

### C11. passthrough mode (remote_oauth_issuer)

Proposed definition: proxy the bearer the MCP client sent us → no Gram-side token storage. Confirm or refine.

**Answer:**
confirmed essentially, but we want this to conform to our abstractions more than we care about this "no Gram-side token storage property". ie. if we must store the token to maintain homogeneity with the remainder of the system then so we shall

---

### C12. implicit vs interactive (client_session_issuer)

Proposed:

- implicit = MCP server is anonymous-only, no consent UI
- interactive = browser-based consent

Confirm or refine.

**Answer:**
close - there can be multiple remote_session_issuers for a single client_session_issuer. After issuing a client session, we must then complete the oauth challenges mandated by the issuer. When implicit is the mode, we just redirect to each subsequent challenge when we return to our callback, build the entire session, and then redirect back to the client callback and await the final token exchannge. For interactive, instead we issue a client session and then we render a UX where the user will click on each remote OAuth server to authenticate there

---

### C13. url mode elicitation (milestone #8)

Proposed: MCP elicitation returning a URL the user opens to refresh stale remote credentials. Confirm.

**Answer:**
yeah and the URL should go to the same challenge screen as "interactive" mode mentioned above

---

### C14. passthrough Authentication (milestone #2) vs passthrough mode (C11)

Same concept or different?

**Answer:**

same

---

### C15. implicit challenge (milestone #3) vs multi-plexing (#4)

Proposed:

- #3 = one remote session per Gram session
- #4 = N remote sessions per Gram session, keyed by issuer

Confirm.

**Answer:**

not exactly - #3 is just that there is no intermediate UI where the user gets redirected to each downstream. There _should_ still be a mechanism for forcing that consent gets prompted _somewhere_ in the request stream


---

### C16. anonymous principal URN format

Proposed: `urn:gram:principal:anonymous:<mcp-session-id>`. OK or different shape?

**Answer:**
right idea for sure, but we need to sure it matches our current urns for api and session principals (this will be the value of `sub` in the oidc JWT that we sign)

---

## D. Architectural decisions

### D17. Issuer ↔ client cardinality

- one `remote_oauth_issuer` → many `remote_oauth_clients`?
- boundary: per project? per org?
- existing `external_oauth_client_registrations` is per-org-per-issuer — keep that boundary?

**Answer:**
I don't know what you mean by `boundary`, but `external_oauth_client_registrations` is essentially an unrelated system. This is for when Gram is acting as an MCP client. It is essentially correct that there should be allowed many remote_oauth_clients at the schema level, though for the purposes of the scope at hand this will never happen. We simply hope to maintain sufficient flexibility so as to decouple the creation of issuers with the connection of Gram servers to those oauth providers. Most notibly this gives us a vehicle to have multiple client credentials for instance for a single share Notion MCP.

---

### D18. Which legacy tables get split?

My read:

- `oauth_proxy_providers` → `remote_oauth_issuer` + `remote_oauth_client`
- `external_oauth_server_metadata` is already an issuer record → just rename

Confirm or correct.

**Answer:**
correct for first point. second isn't a _just_ rename type of thing. we still want an issuer table, but we want it to be nice and strongly typed and all that jazz

---

### D19. Access-token storage post-rewrite

Proposed: access token = signed JWT (no Redis read on validate); refresh token = Redis doc keyed on `(session_id, X)`. What is X?

- (a) `client_session_issuer_id`
- (b) `toolset_id`
- (c) `client_id`
- (d) something else

**Answer:**
client_session_issuer_id for sure. This should have a 1:1 relationship with toolset for now, but in the future it may allow us to share sessions across multiple toolsets to make for more powerful shared sessions

---

### D20. JWT signing

- (a) reuse `GRAM_JWT_SIGNING_KEY` + HS256 with a different `aud`
- (b) new key / alg for client session tokens

**Answer:**

(a) though I don't understand why we need a different "aud"? I guess the aud is really just our toolset slug, but idk if that matters all that much

---

### D21. Out of scope: "OAuth sessions in the Gram playground"

- (a) leave `user_oauth_tokens` table entirely alone
- (b) playground UX is untouched but the underlying table can migrate

**Answer:**
nah leave it alone. it should effectively be unrelated

---

## E. Skills (step 1.5)

### E22. Five skills (Gram, Gram Legacy OAuth, MCP F/B Decoupling, MCP spec, OAuth)

- each as its own subdir under `.claude/skills/`?
- draft serially (you review each) or all at once?

**Answer:**
serially please. 

---

### E23. "MCP Frontend/Backend Decoupling"

- internal-only Gram concept?
- got Notion / GitHub links you want pinned in the skill?

**Answer:**

- internal only Gram concept
- https://www.notion.so/RFC-Gram-MCP-Frontends-and-Slugs-342726c497cc800ba609de5cbe5f3d38?source=copy_link
- https://www.notion.so/speakeasyapi/RFC-Gram-Remote-MCP-Servers-33c726c497cc8072ac6dc6816f3d264f?source=copy_link
- https://github.com/speakeasy-api/gram/pull/2412

---
