import * as z from "zod/v4-mini";
export type ListAssetsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListAssetsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListAssetsSecurity = {
  option1?: ListAssetsSecurityOption1 | undefined;
  option2?: ListAssetsSecurityOption2 | undefined;
};
export type ListAssetsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type ListAssetsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListAssetsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListAssetsSecurityOption1$Outbound,
  ListAssetsSecurityOption1
>;
export declare function listAssetsSecurityOption1ToJSON(
  listAssetsSecurityOption1: ListAssetsSecurityOption1,
): string;
/** @internal */
export type ListAssetsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListAssetsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListAssetsSecurityOption2$Outbound,
  ListAssetsSecurityOption2
>;
export declare function listAssetsSecurityOption2ToJSON(
  listAssetsSecurityOption2: ListAssetsSecurityOption2,
): string;
/** @internal */
export type ListAssetsSecurity$Outbound = {
  Option1?: ListAssetsSecurityOption1$Outbound | undefined;
  Option2?: ListAssetsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListAssetsSecurity$outboundSchema: z.ZodMiniType<
  ListAssetsSecurity$Outbound,
  ListAssetsSecurity
>;
export declare function listAssetsSecurityToJSON(
  listAssetsSecurity: ListAssetsSecurity,
): string;
/** @internal */
export type ListAssetsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListAssetsRequest$outboundSchema: z.ZodMiniType<
  ListAssetsRequest$Outbound,
  ListAssetsRequest
>;
export declare function listAssetsRequestToJSON(
  listAssetsRequest: ListAssetsRequest,
): string;
//# sourceMappingURL=listassets.d.ts.map
