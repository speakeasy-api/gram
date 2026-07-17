import * as z from "zod/v4-mini";
export type GetAgentPluginsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
};
export type GetAgentPluginsRequest = {
  /**
   * Email address of the enrolled user. Used to resolve plugin assignments against principal URNs.
   */
  email: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type GetAgentPluginsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetAgentPluginsSecurity$outboundSchema: z.ZodMiniType<
  GetAgentPluginsSecurity$Outbound,
  GetAgentPluginsSecurity
>;
export declare function getAgentPluginsSecurityToJSON(
  getAgentPluginsSecurity: GetAgentPluginsSecurity,
): string;
/** @internal */
export type GetAgentPluginsRequest$Outbound = {
  email: string;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetAgentPluginsRequest$outboundSchema: z.ZodMiniType<
  GetAgentPluginsRequest$Outbound,
  GetAgentPluginsRequest
>;
export declare function getAgentPluginsRequestToJSON(
  getAgentPluginsRequest: GetAgentPluginsRequest,
): string;
//# sourceMappingURL=getagentplugins.d.ts.map
