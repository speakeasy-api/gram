import * as z from "zod/v4-mini";
export type CreateGlobalToolVariationGroupSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateGlobalToolVariationGroupSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateGlobalToolVariationGroupSecurity = {
  option1?: CreateGlobalToolVariationGroupSecurityOption1 | undefined;
  option2?: CreateGlobalToolVariationGroupSecurityOption2 | undefined;
};
export type CreateGlobalToolVariationGroupRequest = {
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
export type CreateGlobalToolVariationGroupSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateGlobalToolVariationGroupSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateGlobalToolVariationGroupSecurityOption1$Outbound,
  CreateGlobalToolVariationGroupSecurityOption1
>;
export declare function createGlobalToolVariationGroupSecurityOption1ToJSON(
  createGlobalToolVariationGroupSecurityOption1: CreateGlobalToolVariationGroupSecurityOption1,
): string;
/** @internal */
export type CreateGlobalToolVariationGroupSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateGlobalToolVariationGroupSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateGlobalToolVariationGroupSecurityOption2$Outbound,
  CreateGlobalToolVariationGroupSecurityOption2
>;
export declare function createGlobalToolVariationGroupSecurityOption2ToJSON(
  createGlobalToolVariationGroupSecurityOption2: CreateGlobalToolVariationGroupSecurityOption2,
): string;
/** @internal */
export type CreateGlobalToolVariationGroupSecurity$Outbound = {
  Option1?: CreateGlobalToolVariationGroupSecurityOption1$Outbound | undefined;
  Option2?: CreateGlobalToolVariationGroupSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateGlobalToolVariationGroupSecurity$outboundSchema: z.ZodMiniType<
  CreateGlobalToolVariationGroupSecurity$Outbound,
  CreateGlobalToolVariationGroupSecurity
>;
export declare function createGlobalToolVariationGroupSecurityToJSON(
  createGlobalToolVariationGroupSecurity: CreateGlobalToolVariationGroupSecurity,
): string;
/** @internal */
export type CreateGlobalToolVariationGroupRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CreateGlobalToolVariationGroupRequest$outboundSchema: z.ZodMiniType<
  CreateGlobalToolVariationGroupRequest$Outbound,
  CreateGlobalToolVariationGroupRequest
>;
export declare function createGlobalToolVariationGroupRequestToJSON(
  createGlobalToolVariationGroupRequest: CreateGlobalToolVariationGroupRequest,
): string;
//# sourceMappingURL=createglobaltoolvariationgroup.d.ts.map
