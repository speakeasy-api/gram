import type {
  OAuthProxyProvider,
  RemoteSessionClient,
  RemoteSessionIssuer,
  UserSessionIssuer,
} from "@gram/client/models/components";

import type { MigrationDefaults } from "./defaults";

export type MigrationStepKey =
  | "userSessionIssuer"
  | "remoteSessionIssuer"
  | "remoteSessionClient";

export type StepStatus = "pending" | "running" | "done" | "error";

export type MigrationParadigm = "gram" | "custom";

// Three ways the remote_session_client can be created:
//   - clone:    Read the upstream client_id / client_secret from the existing
//               oauth_proxy_provider and reuse it. Preserves the IdP-side
//               registration (including its registered redirect URIs).
//   - register: Run RFC 7591 Dynamic Client Registration against the issuer's
//               registration_endpoint. Mints a fresh upstream client. Only
//               available when the issuer advertises a registration endpoint.
//   - manual:   Operator pastes the client_id / client_secret they already
//               registered out-of-band with the upstream IdP.
export type ClientStrategy = "clone" | "register" | "manual";

export type MigrationStep = {
  key: MigrationStepKey;
  /** Short resource name, e.g. "User Session Issuer". */
  resourceLabel: string;
  /** Verb-led summary of what this step does. */
  description: string;
  status: StepStatus;
  error: string | null;
  resultId: string | null;
};

export type MigrationFormState = {
  userSessionIssuerSlug: string;
  remoteSessionIssuerSlug: string;
  issuerUrl: string;
  sessionDurationHours: number;
  /** Which path to use for the remote_session_client step. null = chooser. */
  clientStrategy: ClientStrategy | null;
  /** Gate on the clone path — operator confirms they've registered the new callback. */
  cloneCallbackConfirmed: boolean;
  /** Manual-path inputs. */
  manualClientId: string;
  manualClientSecret: string;
  manualClientName: string;
};

export type MigrationContext = {
  defaults: MigrationDefaults;
  paradigm: MigrationParadigm;
  toolsetSlug: string;
  form: MigrationFormState;
  userSessionIssuer: UserSessionIssuer | null;
  remoteSessionIssuer: RemoteSessionIssuer | null;
  remoteSessionClient: RemoteSessionClient | null;
  toolsetLinked: boolean;
  error: string | null;
  errorStep: MigrationStepKey | null;
};

export type MigrationInput = {
  defaults: MigrationDefaults;
  paradigm: MigrationParadigm;
  toolsetSlug: string;
  existingUserSessionIssuer?: UserSessionIssuer | null;
  existingRemoteSessionIssuer?: RemoteSessionIssuer | null;
  existingRemoteSessionClient?: RemoteSessionClient | null;
  toolsetLinked?: boolean;
};

export type MigrationEvent =
  | { type: "FORM"; patch: Partial<MigrationFormState> }
  | { type: "SUBMIT" };

export type ResolveUserSessionIssuerInput = {
  slug: string;
};

export type ResolveRemoteSessionIssuerInput = {
  slug: string;
};

export type ResolveRemoteSessionClientInput = {
  userSessionIssuerId: string;
  remoteSessionIssuerId: string;
};

export type CreateUserSessionIssuerInput = {
  slug: string;
  sessionDurationHours: number;
};

export type CreateRemoteSessionIssuerInput = {
  slug: string;
  issuerUrl: string;
  proxyProvider: OAuthProxyProvider;
};

export type CreateRemoteSessionClientInput = {
  userSessionIssuerId: string;
  remoteSessionIssuer: RemoteSessionIssuer;
  proxyProviderId: string;
  strategy: ClientStrategy;
  cloneCallbackConfirmed: boolean;
  manualClientId: string;
  manualClientSecret: string;
  manualClientName: string;
};

export type LinkToolsetUserSessionIssuerInput = {
  toolsetSlug: string;
  userSessionIssuerId: string;
};
