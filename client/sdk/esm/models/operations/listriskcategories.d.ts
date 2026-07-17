import * as z from "zod/v4-mini";
export type ListRiskCategoriesSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRiskCategoriesSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRiskCategoriesSecurity = {
  option1?: ListRiskCategoriesSecurityOption1 | undefined;
  option2?: ListRiskCategoriesSecurityOption2 | undefined;
};
export type ListRiskCategoriesRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
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
export type ListRiskCategoriesSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskCategoriesSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRiskCategoriesSecurityOption1$Outbound,
  ListRiskCategoriesSecurityOption1
>;
export declare function listRiskCategoriesSecurityOption1ToJSON(
  listRiskCategoriesSecurityOption1: ListRiskCategoriesSecurityOption1,
): string;
/** @internal */
export type ListRiskCategoriesSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskCategoriesSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRiskCategoriesSecurityOption2$Outbound,
  ListRiskCategoriesSecurityOption2
>;
export declare function listRiskCategoriesSecurityOption2ToJSON(
  listRiskCategoriesSecurityOption2: ListRiskCategoriesSecurityOption2,
): string;
/** @internal */
export type ListRiskCategoriesSecurity$Outbound = {
  Option1?: ListRiskCategoriesSecurityOption1$Outbound | undefined;
  Option2?: ListRiskCategoriesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskCategoriesSecurity$outboundSchema: z.ZodMiniType<
  ListRiskCategoriesSecurity$Outbound,
  ListRiskCategoriesSecurity
>;
export declare function listRiskCategoriesSecurityToJSON(
  listRiskCategoriesSecurity: ListRiskCategoriesSecurity,
): string;
/** @internal */
export type ListRiskCategoriesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskCategoriesRequest$outboundSchema: z.ZodMiniType<
  ListRiskCategoriesRequest$Outbound,
  ListRiskCategoriesRequest
>;
export declare function listRiskCategoriesRequestToJSON(
  listRiskCategoriesRequest: ListRiskCategoriesRequest,
): string;
//# sourceMappingURL=listriskcategories.d.ts.map
