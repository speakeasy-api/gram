import * as z from "zod/v4-mini";
import {
  SetToolVariationsGroupRequestBody,
  SetToolVariationsGroupRequestBody$Outbound,
} from "../components/settoolvariationsgrouprequestbody.js";
export type SetToolsetToolVariationsGroupSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SetToolsetToolVariationsGroupSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SetToolsetToolVariationsGroupSecurity = {
  option1?: SetToolsetToolVariationsGroupSecurityOption1 | undefined;
  option2?: SetToolsetToolVariationsGroupSecurityOption2 | undefined;
};
export type SetToolsetToolVariationsGroupRequest = {
  /**
   * The slug of the toolset to configure
   */
  slug: string;
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
  setToolVariationsGroupRequestBody: SetToolVariationsGroupRequestBody;
};
/** @internal */
export type SetToolsetToolVariationsGroupSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SetToolsetToolVariationsGroupSecurityOption1$outboundSchema: z.ZodMiniType<
  SetToolsetToolVariationsGroupSecurityOption1$Outbound,
  SetToolsetToolVariationsGroupSecurityOption1
>;
export declare function setToolsetToolVariationsGroupSecurityOption1ToJSON(
  setToolsetToolVariationsGroupSecurityOption1: SetToolsetToolVariationsGroupSecurityOption1,
): string;
/** @internal */
export type SetToolsetToolVariationsGroupSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SetToolsetToolVariationsGroupSecurityOption2$outboundSchema: z.ZodMiniType<
  SetToolsetToolVariationsGroupSecurityOption2$Outbound,
  SetToolsetToolVariationsGroupSecurityOption2
>;
export declare function setToolsetToolVariationsGroupSecurityOption2ToJSON(
  setToolsetToolVariationsGroupSecurityOption2: SetToolsetToolVariationsGroupSecurityOption2,
): string;
/** @internal */
export type SetToolsetToolVariationsGroupSecurity$Outbound = {
  Option1?: SetToolsetToolVariationsGroupSecurityOption1$Outbound | undefined;
  Option2?: SetToolsetToolVariationsGroupSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SetToolsetToolVariationsGroupSecurity$outboundSchema: z.ZodMiniType<
  SetToolsetToolVariationsGroupSecurity$Outbound,
  SetToolsetToolVariationsGroupSecurity
>;
export declare function setToolsetToolVariationsGroupSecurityToJSON(
  setToolsetToolVariationsGroupSecurity: SetToolsetToolVariationsGroupSecurity,
): string;
/** @internal */
export type SetToolsetToolVariationsGroupRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SetToolVariationsGroupRequestBody: SetToolVariationsGroupRequestBody$Outbound;
};
/** @internal */
export declare const SetToolsetToolVariationsGroupRequest$outboundSchema: z.ZodMiniType<
  SetToolsetToolVariationsGroupRequest$Outbound,
  SetToolsetToolVariationsGroupRequest
>;
export declare function setToolsetToolVariationsGroupRequestToJSON(
  setToolsetToolVariationsGroupRequest: SetToolsetToolVariationsGroupRequest,
): string;
//# sourceMappingURL=settoolsettoolvariationsgroup.d.ts.map
