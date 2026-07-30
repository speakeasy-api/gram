import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type PublishMCPRegistrySecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type PublishMCPRegistrySecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type PublishMCPRegistrySecurity = {
  option1?: PublishMCPRegistrySecurityOption1 | undefined;
  option2?: PublishMCPRegistrySecurityOption2 | undefined;
};
export type PublishMCPRegistryRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  publishRequestBody: components.PublishRequestBody;
};
/** @internal */
export type PublishMCPRegistrySecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const PublishMCPRegistrySecurityOption1$outboundSchema: z.ZodMiniType<
  PublishMCPRegistrySecurityOption1$Outbound,
  PublishMCPRegistrySecurityOption1
>;
export declare function publishMCPRegistrySecurityOption1ToJSON(
  publishMCPRegistrySecurityOption1: PublishMCPRegistrySecurityOption1,
): string;
/** @internal */
export type PublishMCPRegistrySecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const PublishMCPRegistrySecurityOption2$outboundSchema: z.ZodMiniType<
  PublishMCPRegistrySecurityOption2$Outbound,
  PublishMCPRegistrySecurityOption2
>;
export declare function publishMCPRegistrySecurityOption2ToJSON(
  publishMCPRegistrySecurityOption2: PublishMCPRegistrySecurityOption2,
): string;
/** @internal */
export type PublishMCPRegistrySecurity$Outbound = {
  Option1?: PublishMCPRegistrySecurityOption1$Outbound | undefined;
  Option2?: PublishMCPRegistrySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const PublishMCPRegistrySecurity$outboundSchema: z.ZodMiniType<
  PublishMCPRegistrySecurity$Outbound,
  PublishMCPRegistrySecurity
>;
export declare function publishMCPRegistrySecurityToJSON(
  publishMCPRegistrySecurity: PublishMCPRegistrySecurity,
): string;
/** @internal */
export type PublishMCPRegistryRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  PublishRequestBody: components.PublishRequestBody$Outbound;
};
/** @internal */
export declare const PublishMCPRegistryRequest$outboundSchema: z.ZodMiniType<
  PublishMCPRegistryRequest$Outbound,
  PublishMCPRegistryRequest
>;
export declare function publishMCPRegistryRequestToJSON(
  publishMCPRegistryRequest: PublishMCPRegistryRequest,
): string;
//# sourceMappingURL=publishmcpregistry.d.ts.map
