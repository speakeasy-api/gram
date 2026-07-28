import * as z from "zod/v4-mini";
export type ListAuditLogFacetsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListAuditLogFacetsRequest = {
  /**
   * Project slug to filter facet values to a specific project.
   */
  projectSlug?: string | undefined;
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
export type ListAuditLogFacetsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAuditLogFacetsSecurity$outboundSchema: z.ZodMiniType<
  ListAuditLogFacetsSecurity$Outbound,
  ListAuditLogFacetsSecurity
>;
export declare function listAuditLogFacetsSecurityToJSON(
  listAuditLogFacetsSecurity: ListAuditLogFacetsSecurity,
): string;
/** @internal */
export type ListAuditLogFacetsRequest$Outbound = {
  project_slug?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAuditLogFacetsRequest$outboundSchema: z.ZodMiniType<
  ListAuditLogFacetsRequest$Outbound,
  ListAuditLogFacetsRequest
>;
export declare function listAuditLogFacetsRequestToJSON(
  listAuditLogFacetsRequest: ListAuditLogFacetsRequest,
): string;
//# sourceMappingURL=listauditlogfacets.d.ts.map
