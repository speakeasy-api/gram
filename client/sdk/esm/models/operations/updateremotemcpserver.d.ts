import * as z from "zod/v4-mini";
import {
  UpdateServerForm,
  UpdateServerForm$Outbound,
} from "../components/updateserverform.js";
export type UpdateRemoteMcpServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateRemoteMcpServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateRemoteMcpServerSecurity = {
  option1?: UpdateRemoteMcpServerSecurityOption1 | undefined;
  option2?: UpdateRemoteMcpServerSecurityOption2 | undefined;
};
export type UpdateRemoteMcpServerRequest = {
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
  updateServerForm: UpdateServerForm;
};
/** @internal */
export type UpdateRemoteMcpServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateRemoteMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<
  UpdateRemoteMcpServerSecurityOption1$Outbound,
  UpdateRemoteMcpServerSecurityOption1
>;
export declare function updateRemoteMcpServerSecurityOption1ToJSON(
  updateRemoteMcpServerSecurityOption1: UpdateRemoteMcpServerSecurityOption1,
): string;
/** @internal */
export type UpdateRemoteMcpServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateRemoteMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<
  UpdateRemoteMcpServerSecurityOption2$Outbound,
  UpdateRemoteMcpServerSecurityOption2
>;
export declare function updateRemoteMcpServerSecurityOption2ToJSON(
  updateRemoteMcpServerSecurityOption2: UpdateRemoteMcpServerSecurityOption2,
): string;
/** @internal */
export type UpdateRemoteMcpServerSecurity$Outbound = {
  Option1?: UpdateRemoteMcpServerSecurityOption1$Outbound | undefined;
  Option2?: UpdateRemoteMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateRemoteMcpServerSecurity$outboundSchema: z.ZodMiniType<
  UpdateRemoteMcpServerSecurity$Outbound,
  UpdateRemoteMcpServerSecurity
>;
export declare function updateRemoteMcpServerSecurityToJSON(
  updateRemoteMcpServerSecurity: UpdateRemoteMcpServerSecurity,
): string;
/** @internal */
export type UpdateRemoteMcpServerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateServerForm: UpdateServerForm$Outbound;
};
/** @internal */
export declare const UpdateRemoteMcpServerRequest$outboundSchema: z.ZodMiniType<
  UpdateRemoteMcpServerRequest$Outbound,
  UpdateRemoteMcpServerRequest
>;
export declare function updateRemoteMcpServerRequestToJSON(
  updateRemoteMcpServerRequest: UpdateRemoteMcpServerRequest,
): string;
//# sourceMappingURL=updateremotemcpserver.d.ts.map
