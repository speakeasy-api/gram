import * as z from "zod/v4-mini";
export type ListVersionsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListVersionsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListVersionsSecurity = {
  option1?: ListVersionsSecurityOption1 | undefined;
  option2?: ListVersionsSecurityOption2 | undefined;
};
export type ListVersionsRequest = {
  /**
   * The name of the package
   */
  name: string;
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
export type ListVersionsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListVersionsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListVersionsSecurityOption1$Outbound,
  ListVersionsSecurityOption1
>;
export declare function listVersionsSecurityOption1ToJSON(
  listVersionsSecurityOption1: ListVersionsSecurityOption1,
): string;
/** @internal */
export type ListVersionsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListVersionsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListVersionsSecurityOption2$Outbound,
  ListVersionsSecurityOption2
>;
export declare function listVersionsSecurityOption2ToJSON(
  listVersionsSecurityOption2: ListVersionsSecurityOption2,
): string;
/** @internal */
export type ListVersionsSecurity$Outbound = {
  Option1?: ListVersionsSecurityOption1$Outbound | undefined;
  Option2?: ListVersionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListVersionsSecurity$outboundSchema: z.ZodMiniType<
  ListVersionsSecurity$Outbound,
  ListVersionsSecurity
>;
export declare function listVersionsSecurityToJSON(
  listVersionsSecurity: ListVersionsSecurity,
): string;
/** @internal */
export type ListVersionsRequest$Outbound = {
  name: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListVersionsRequest$outboundSchema: z.ZodMiniType<
  ListVersionsRequest$Outbound,
  ListVersionsRequest
>;
export declare function listVersionsRequestToJSON(
  listVersionsRequest: ListVersionsRequest,
): string;
//# sourceMappingURL=listversions.d.ts.map
