import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { ServerIdentityClientConfiguration } from "@gram/client/models/components/serveridentityclientconfiguration.js";
import {
  hasSlackReadScopes,
  isSlackMcpUrl,
  isSlackProvider,
  slackClientMismatch,
} from "./slack";

/** Metadata needed to decide whether a guide can safely configure a provider. */
type OAuthSetupProvider = {
  issuer?: string;
  authorizationEndpoint?: string | null;
  tokenEndpoint?: string | null;
  tokenEndpointAuthMethodsSupported?: string[] | null;
};

/** Pure provider rules. Draft changes and persistence remain in the shared hook. */
export type OAuthSetupPolicy = {
  id: string;
  providerName: string;
  changeConnectionLabel: string;
  matchesUrl: (url: string | undefined) => boolean;
  prefersManualRegistration: boolean;
  requiresClientSecret: boolean;
  tokenEndpointAuthMethod: NonNullable<
    ServerIdentityClientConfiguration["tokenEndpointAuthMethod"]
  >;
  isProviderCompatible: (
    provider: OAuthSetupProvider | null | undefined,
  ) => boolean;
  isScopeSelectionCompatible: (
    scopes: readonly string[] | undefined,
  ) => boolean;
  clientMismatch: (
    client: RemoteSessionClient,
    providerId: string,
  ) => string | null;
  manualConfigurationError: string;
};

export const slackOAuthSetupPolicy: OAuthSetupPolicy = {
  id: "slack",
  providerName: "Slack",
  changeConnectionLabel: "Change Slack app",
  matchesUrl: isSlackMcpUrl,
  prefersManualRegistration: true,
  requiresClientSecret: true,
  tokenEndpointAuthMethod: "client_secret_post",
  isProviderCompatible: isSlackProvider,
  isScopeSelectionCompatible: hasSlackReadScopes,
  clientMismatch: slackClientMismatch,
  manualConfigurationError:
    "Slack setup requires the reviewed provider, supported read/search access, and a client secret.",
};

// Keep this registry independent of React so shared drafts need no guide imports.
export const oauthSetupPolicies: readonly OAuthSetupPolicy[] = [
  slackOAuthSetupPolicy,
];

export function getOAuthSetupPolicy(
  url: string | undefined,
): OAuthSetupPolicy | undefined {
  return oauthSetupPolicies.find((policy) => policy.matchesUrl(url));
}
