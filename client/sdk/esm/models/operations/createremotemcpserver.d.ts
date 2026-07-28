import * as z from "zod/v4-mini";
import {
  CreateServerForm,
  CreateServerForm$Outbound,
} from "../components/createserverform.js";
export type CreateRemoteMcpServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateRemoteMcpServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateRemoteMcpServerSecurity = {
  option1?: CreateRemoteMcpServerSecurityOption1 | undefined;
  option2?: CreateRemoteMcpServerSecurityOption2 | undefined;
};
export type CreateRemoteMcpServerRequest = {
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
  createServerForm: CreateServerForm;
};
/** @internal */
export type CreateRemoteMcpServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateRemoteMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateRemoteMcpServerSecurityOption1$Outbound,
  CreateRemoteMcpServerSecurityOption1
>;
export declare function createRemoteMcpServerSecurityOption1ToJSON(
  createRemoteMcpServerSecurityOption1: CreateRemoteMcpServerSecurityOption1,
): string;
/** @internal */
export type CreateRemoteMcpServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateRemoteMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateRemoteMcpServerSecurityOption2$Outbound,
  CreateRemoteMcpServerSecurityOption2
>;
export declare function createRemoteMcpServerSecurityOption2ToJSON(
  createRemoteMcpServerSecurityOption2: CreateRemoteMcpServerSecurityOption2,
): string;
/** @internal */
export type CreateRemoteMcpServerSecurity$Outbound = {
  Option1?: CreateRemoteMcpServerSecurityOption1$Outbound | undefined;
  Option2?: CreateRemoteMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateRemoteMcpServerSecurity$outboundSchema: z.ZodMiniType<
  CreateRemoteMcpServerSecurity$Outbound,
  CreateRemoteMcpServerSecurity
>;
export declare function createRemoteMcpServerSecurityToJSON(
  createRemoteMcpServerSecurity: CreateRemoteMcpServerSecurity,
): string;
/** @internal */
export type CreateRemoteMcpServerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateServerForm: CreateServerForm$Outbound;
};
/** @internal */
export declare const CreateRemoteMcpServerRequest$outboundSchema: z.ZodMiniType<
  CreateRemoteMcpServerRequest$Outbound,
  CreateRemoteMcpServerRequest
>;
export declare function createRemoteMcpServerRequestToJSON(
  createRemoteMcpServerRequest: CreateRemoteMcpServerRequest,
): string;
//# sourceMappingURL=createremotemcpserver.d.ts.map
