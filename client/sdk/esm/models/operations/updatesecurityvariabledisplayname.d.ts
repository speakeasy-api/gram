import * as z from "zod/v3";
import * as components from "../components/index.js";
export type UpdateSecurityVariableDisplayNameSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateSecurityVariableDisplayNameSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateSecurityVariableDisplayNameSecurity = {
  option1?: UpdateSecurityVariableDisplayNameSecurityOption1 | undefined;
  option2?: UpdateSecurityVariableDisplayNameSecurityOption2 | undefined;
};
export type UpdateSecurityVariableDisplayNameRequest = {
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
  updateSecurityVariableDisplayNameRequestBody: components.UpdateSecurityVariableDisplayNameRequestBody;
};
/** @internal */
export type UpdateSecurityVariableDisplayNameSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateSecurityVariableDisplayNameSecurityOption1$outboundSchema: z.ZodType<
  UpdateSecurityVariableDisplayNameSecurityOption1$Outbound,
  z.ZodTypeDef,
  UpdateSecurityVariableDisplayNameSecurityOption1
>;
export declare function updateSecurityVariableDisplayNameSecurityOption1ToJSON(
  updateSecurityVariableDisplayNameSecurityOption1: UpdateSecurityVariableDisplayNameSecurityOption1,
): string;
/** @internal */
export type UpdateSecurityVariableDisplayNameSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateSecurityVariableDisplayNameSecurityOption2$outboundSchema: z.ZodType<
  UpdateSecurityVariableDisplayNameSecurityOption2$Outbound,
  z.ZodTypeDef,
  UpdateSecurityVariableDisplayNameSecurityOption2
>;
export declare function updateSecurityVariableDisplayNameSecurityOption2ToJSON(
  updateSecurityVariableDisplayNameSecurityOption2: UpdateSecurityVariableDisplayNameSecurityOption2,
): string;
/** @internal */
export type UpdateSecurityVariableDisplayNameSecurity$Outbound = {
  Option1?:
    | UpdateSecurityVariableDisplayNameSecurityOption1$Outbound
    | undefined;
  Option2?:
    | UpdateSecurityVariableDisplayNameSecurityOption2$Outbound
    | undefined;
};
/** @internal */
export declare const UpdateSecurityVariableDisplayNameSecurity$outboundSchema: z.ZodType<
  UpdateSecurityVariableDisplayNameSecurity$Outbound,
  z.ZodTypeDef,
  UpdateSecurityVariableDisplayNameSecurity
>;
export declare function updateSecurityVariableDisplayNameSecurityToJSON(
  updateSecurityVariableDisplayNameSecurity: UpdateSecurityVariableDisplayNameSecurity,
): string;
/** @internal */
export type UpdateSecurityVariableDisplayNameRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateSecurityVariableDisplayNameRequestBody: components.UpdateSecurityVariableDisplayNameRequestBody$Outbound;
};
/** @internal */
export declare const UpdateSecurityVariableDisplayNameRequest$outboundSchema: z.ZodType<
  UpdateSecurityVariableDisplayNameRequest$Outbound,
  z.ZodTypeDef,
  UpdateSecurityVariableDisplayNameRequest
>;
export declare function updateSecurityVariableDisplayNameRequestToJSON(
  updateSecurityVariableDisplayNameRequest: UpdateSecurityVariableDisplayNameRequest,
): string;
//# sourceMappingURL=updatesecurityvariabledisplayname.d.ts.map
