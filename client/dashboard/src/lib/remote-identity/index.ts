/**
 * Remote identity: models, hooks, and components for presenting the upstream
 * identity provider, the static-credential alternative, and the absence of
 * both as one coherent choice. See README.md.
 *
 * Every export is named on purpose, and this list is exactly what the module's
 * consumers import — not everything the module happens to define. Internals
 * stay internal until something outside needs them.
 */
export { deriveIdentityMode } from "./model/identity";
export type { IdentityMode } from "./model/identity";

export {
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
  managedAuthorizationHeader,
} from "./model/headers";

export { REDACTED_SECRET } from "./model/secret";

export {
  headerDraftErrors,
  headerDraftFromCatalog,
} from "./drafts/headerDrafts";
export type {
  HeaderDraft,
  HeaderDraftError,
  HeaderSource,
} from "./drafts/headerDrafts";

export { useHeaderDrafts } from "./drafts/useHeaderDrafts";
export type { HeaderDraftsState } from "./drafts/useHeaderDrafts";

export {
  useAgentCredentialDraft,
  useAgentCredentialFields,
} from "./drafts/useCredentialDraft";
export type { AgentCredentialFields } from "./drafts/useCredentialDraft";

export { useUserIdentityDraft } from "./drafts/useIdentityDraft";

export { useAllRemoteSessionClients } from "./queries/useAllRemoteSessionClients";
export { useUpstreamProbe } from "./queries/useUpstreamProbe";

export { AgentIdentityRow } from "./components/CredentialFields";
export {
  IdentityExplainerCallout,
  IdentityExplainerDialog,
} from "./components/IdentityExplainer";
export { identityModeCards } from "./components/IdentityModeCards";
export { IssuerLink } from "./components/ProviderLink";
export { UserIdentityRow } from "./components/ProviderRow";
export { ScopeBadge } from "./components/ScopeBadge";
