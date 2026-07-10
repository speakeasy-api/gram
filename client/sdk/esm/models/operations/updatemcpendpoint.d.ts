import * as z from "zod/v4-mini";
import {
  UpdateMcpEndpointForm,
  UpdateMcpEndpointForm$Outbound,
} from "../components/updatemcpendpointform.js";
export type UpdateMcpEndpointSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateMcpEndpointSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateMcpEndpointSecurity = {
  option1?: UpdateMcpEndpointSecurityOption1 | undefined;
  option2?: UpdateMcpEndpointSecurityOption2 | undefined;
};
export type UpdateMcpEndpointRequest = {
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
  updateMcpEndpointForm: UpdateMcpEndpointForm;
};
/** @internal */
export type UpdateMcpEndpointSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateMcpEndpointSecurityOption1$outboundSchema: z.ZodMiniType<
  UpdateMcpEndpointSecurityOption1$Outbound,
  UpdateMcpEndpointSecurityOption1
>;
export declare function updateMcpEndpointSecurityOption1ToJSON(
  updateMcpEndpointSecurityOption1: UpdateMcpEndpointSecurityOption1,
): string;
/** @internal */
export type UpdateMcpEndpointSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateMcpEndpointSecurityOption2$outboundSchema: z.ZodMiniType<
  UpdateMcpEndpointSecurityOption2$Outbound,
  UpdateMcpEndpointSecurityOption2
>;
export declare function updateMcpEndpointSecurityOption2ToJSON(
  updateMcpEndpointSecurityOption2: UpdateMcpEndpointSecurityOption2,
): string;
/** @internal */
export type UpdateMcpEndpointSecurity$Outbound = {
  Option1?: UpdateMcpEndpointSecurityOption1$Outbound | undefined;
  Option2?: UpdateMcpEndpointSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateMcpEndpointSecurity$outboundSchema: z.ZodMiniType<
  UpdateMcpEndpointSecurity$Outbound,
  UpdateMcpEndpointSecurity
>;
export declare function updateMcpEndpointSecurityToJSON(
  updateMcpEndpointSecurity: UpdateMcpEndpointSecurity,
): string;
/** @internal */
export type UpdateMcpEndpointRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateMcpEndpointForm: UpdateMcpEndpointForm$Outbound;
};
/** @internal */
export declare const UpdateMcpEndpointRequest$outboundSchema: z.ZodMiniType<
  UpdateMcpEndpointRequest$Outbound,
  UpdateMcpEndpointRequest
>;
export declare function updateMcpEndpointRequestToJSON(
  updateMcpEndpointRequest: UpdateMcpEndpointRequest,
): string;
//# sourceMappingURL=updatemcpendpoint.d.ts.map
