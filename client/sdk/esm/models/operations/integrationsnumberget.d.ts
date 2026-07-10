import * as z from "zod/v4-mini";
export type IntegrationsNumberGetSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type IntegrationsNumberGetRequest = {
  /**
   * The ID of the integration to get (refers to a package id).
   */
  id?: string | undefined;
  /**
   * The name of the integration to get (refers to a package name).
   */
  name?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type IntegrationsNumberGetSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const IntegrationsNumberGetSecurity$outboundSchema: z.ZodMiniType<
  IntegrationsNumberGetSecurity$Outbound,
  IntegrationsNumberGetSecurity
>;
export declare function integrationsNumberGetSecurityToJSON(
  integrationsNumberGetSecurity: IntegrationsNumberGetSecurity,
): string;
/** @internal */
export type IntegrationsNumberGetRequest$Outbound = {
  id?: string | undefined;
  name?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const IntegrationsNumberGetRequest$outboundSchema: z.ZodMiniType<
  IntegrationsNumberGetRequest$Outbound,
  IntegrationsNumberGetRequest
>;
export declare function integrationsNumberGetRequestToJSON(
  integrationsNumberGetRequest: IntegrationsNumberGetRequest,
): string;
//# sourceMappingURL=integrationsnumberget.d.ts.map
