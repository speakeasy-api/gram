import * as z from "zod/v4-mini";
import {
  UpdateTunneledMcpServerForm,
  UpdateTunneledMcpServerForm$Outbound,
} from "../components/updatetunneledmcpserverform.js";
export type UpdateTunneledMcpServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateTunneledMcpServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateTunneledMcpServerSecurity = {
  option1?: UpdateTunneledMcpServerSecurityOption1 | undefined;
  option2?: UpdateTunneledMcpServerSecurityOption2 | undefined;
};
export type UpdateTunneledMcpServerRequest = {
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
  updateTunneledMcpServerForm: UpdateTunneledMcpServerForm;
};
/** @internal */
export type UpdateTunneledMcpServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateTunneledMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<
  UpdateTunneledMcpServerSecurityOption1$Outbound,
  UpdateTunneledMcpServerSecurityOption1
>;
export declare function updateTunneledMcpServerSecurityOption1ToJSON(
  updateTunneledMcpServerSecurityOption1: UpdateTunneledMcpServerSecurityOption1,
): string;
/** @internal */
export type UpdateTunneledMcpServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateTunneledMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<
  UpdateTunneledMcpServerSecurityOption2$Outbound,
  UpdateTunneledMcpServerSecurityOption2
>;
export declare function updateTunneledMcpServerSecurityOption2ToJSON(
  updateTunneledMcpServerSecurityOption2: UpdateTunneledMcpServerSecurityOption2,
): string;
/** @internal */
export type UpdateTunneledMcpServerSecurity$Outbound = {
  Option1?: UpdateTunneledMcpServerSecurityOption1$Outbound | undefined;
  Option2?: UpdateTunneledMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateTunneledMcpServerSecurity$outboundSchema: z.ZodMiniType<
  UpdateTunneledMcpServerSecurity$Outbound,
  UpdateTunneledMcpServerSecurity
>;
export declare function updateTunneledMcpServerSecurityToJSON(
  updateTunneledMcpServerSecurity: UpdateTunneledMcpServerSecurity,
): string;
/** @internal */
export type UpdateTunneledMcpServerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateTunneledMcpServerForm: UpdateTunneledMcpServerForm$Outbound;
};
/** @internal */
export declare const UpdateTunneledMcpServerRequest$outboundSchema: z.ZodMiniType<
  UpdateTunneledMcpServerRequest$Outbound,
  UpdateTunneledMcpServerRequest
>;
export declare function updateTunneledMcpServerRequestToJSON(
  updateTunneledMcpServerRequest: UpdateTunneledMcpServerRequest,
): string;
//# sourceMappingURL=updatetunneledmcpserver.d.ts.map
