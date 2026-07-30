import * as z from "zod/v4-mini";
export type ServeMCPRegistrySecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ServeMCPRegistrySecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ServeMCPRegistrySecurity = {
  option1?: ServeMCPRegistrySecurityOption1 | undefined;
  option2?: ServeMCPRegistrySecurityOption2 | undefined;
};
export type ServeMCPRegistryRequest = {
  /**
   * Slug of the registry to serve
   */
  registrySlug: string;
  /**
   * Search query to filter servers by name
   */
  search?: string | undefined;
  /**
   * Pagination cursor
   */
  cursor?: string | undefined;
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
};
/** @internal */
export type ServeMCPRegistrySecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ServeMCPRegistrySecurityOption1$outboundSchema: z.ZodMiniType<
  ServeMCPRegistrySecurityOption1$Outbound,
  ServeMCPRegistrySecurityOption1
>;
export declare function serveMCPRegistrySecurityOption1ToJSON(
  serveMCPRegistrySecurityOption1: ServeMCPRegistrySecurityOption1,
): string;
/** @internal */
export type ServeMCPRegistrySecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ServeMCPRegistrySecurityOption2$outboundSchema: z.ZodMiniType<
  ServeMCPRegistrySecurityOption2$Outbound,
  ServeMCPRegistrySecurityOption2
>;
export declare function serveMCPRegistrySecurityOption2ToJSON(
  serveMCPRegistrySecurityOption2: ServeMCPRegistrySecurityOption2,
): string;
/** @internal */
export type ServeMCPRegistrySecurity$Outbound = {
  Option1?: ServeMCPRegistrySecurityOption1$Outbound | undefined;
  Option2?: ServeMCPRegistrySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ServeMCPRegistrySecurity$outboundSchema: z.ZodMiniType<
  ServeMCPRegistrySecurity$Outbound,
  ServeMCPRegistrySecurity
>;
export declare function serveMCPRegistrySecurityToJSON(
  serveMCPRegistrySecurity: ServeMCPRegistrySecurity,
): string;
/** @internal */
export type ServeMCPRegistryRequest$Outbound = {
  registry_slug: string;
  search?: string | undefined;
  cursor?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ServeMCPRegistryRequest$outboundSchema: z.ZodMiniType<
  ServeMCPRegistryRequest$Outbound,
  ServeMCPRegistryRequest
>;
export declare function serveMCPRegistryRequestToJSON(
  serveMCPRegistryRequest: ServeMCPRegistryRequest,
): string;
//# sourceMappingURL=servemcpregistry.d.ts.map
