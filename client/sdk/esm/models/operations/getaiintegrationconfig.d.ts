import * as z from "zod/v4-mini";
export type GetAIIntegrationConfigSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetAIIntegrationConfigRequest = {
  /**
   * AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.
   */
  provider: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetAIIntegrationConfigSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAIIntegrationConfigSecurity$outboundSchema: z.ZodMiniType<
  GetAIIntegrationConfigSecurity$Outbound,
  GetAIIntegrationConfigSecurity
>;
export declare function getAIIntegrationConfigSecurityToJSON(
  getAIIntegrationConfigSecurity: GetAIIntegrationConfigSecurity,
): string;
/** @internal */
export type GetAIIntegrationConfigRequest$Outbound = {
  provider: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAIIntegrationConfigRequest$outboundSchema: z.ZodMiniType<
  GetAIIntegrationConfigRequest$Outbound,
  GetAIIntegrationConfigRequest
>;
export declare function getAIIntegrationConfigRequestToJSON(
  getAIIntegrationConfigRequest: GetAIIntegrationConfigRequest,
): string;
//# sourceMappingURL=getaiintegrationconfig.d.ts.map
