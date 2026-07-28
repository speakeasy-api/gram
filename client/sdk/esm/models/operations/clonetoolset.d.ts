import * as z from "zod/v4-mini";
export type CloneToolsetSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CloneToolsetSecurityOption2 = {
  apikeyHeaderAuthorization: string;
  projectSlugHeaderGramProject: string;
};
export type CloneToolsetSecurity = {
  option1?: CloneToolsetSecurityOption1 | undefined;
  option2?: CloneToolsetSecurityOption2 | undefined;
};
export type CloneToolsetRequest = {
  /**
   * The slug of the toolset to clone
   */
  slug: string;
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
export type CloneToolsetSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CloneToolsetSecurityOption1$outboundSchema: z.ZodMiniType<
  CloneToolsetSecurityOption1$Outbound,
  CloneToolsetSecurityOption1
>;
export declare function cloneToolsetSecurityOption1ToJSON(
  cloneToolsetSecurityOption1: CloneToolsetSecurityOption1,
): string;
/** @internal */
export type CloneToolsetSecurityOption2$Outbound = {
  apikey_header_Authorization: string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CloneToolsetSecurityOption2$outboundSchema: z.ZodMiniType<
  CloneToolsetSecurityOption2$Outbound,
  CloneToolsetSecurityOption2
>;
export declare function cloneToolsetSecurityOption2ToJSON(
  cloneToolsetSecurityOption2: CloneToolsetSecurityOption2,
): string;
/** @internal */
export type CloneToolsetSecurity$Outbound = {
  Option1?: CloneToolsetSecurityOption1$Outbound | undefined;
  Option2?: CloneToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CloneToolsetSecurity$outboundSchema: z.ZodMiniType<
  CloneToolsetSecurity$Outbound,
  CloneToolsetSecurity
>;
export declare function cloneToolsetSecurityToJSON(
  cloneToolsetSecurity: CloneToolsetSecurity,
): string;
/** @internal */
export type CloneToolsetRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CloneToolsetRequest$outboundSchema: z.ZodMiniType<
  CloneToolsetRequest$Outbound,
  CloneToolsetRequest
>;
export declare function cloneToolsetRequestToJSON(
  cloneToolsetRequest: CloneToolsetRequest,
): string;
//# sourceMappingURL=clonetoolset.d.ts.map
