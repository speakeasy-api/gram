import * as z from "zod/v4-mini";
/**
 * Form for migrating legacy gram OAuth-proxy client registrations onto a user_session_issuer.
 */
export type MigrateLegacyGramRegistrationsForm = {
  /**
   * The gram-type oauth_proxy_provider whose Redis registrations are migrated.
   */
  oauthProxyProviderId: string;
  /**
   * The target user_session_issuer the migrated user_session_clients attach to.
   */
  userSessionIssuerId: string;
};
/** @internal */
export type MigrateLegacyGramRegistrationsForm$Outbound = {
  oauth_proxy_provider_id: string;
  user_session_issuer_id: string;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsForm$outboundSchema: z.ZodMiniType<
  MigrateLegacyGramRegistrationsForm$Outbound,
  MigrateLegacyGramRegistrationsForm
>;
export declare function migrateLegacyGramRegistrationsFormToJSON(
  migrateLegacyGramRegistrationsForm: MigrateLegacyGramRegistrationsForm,
): string;
//# sourceMappingURL=migratelegacygramregistrationsform.d.ts.map
