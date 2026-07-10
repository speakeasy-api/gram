import * as z from "zod/v4-mini";
import {
  CreateRemoteSessionClientForm,
  CreateRemoteSessionClientForm$Outbound,
} from "../components/createremotesessionclientform.js";
export type CreateRemoteSessionClientSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateRemoteSessionClientSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateRemoteSessionClientSecurity = {
  option1?: CreateRemoteSessionClientSecurityOption1 | undefined;
  option2?: CreateRemoteSessionClientSecurityOption2 | undefined;
};
export type CreateRemoteSessionClientRequest = {
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
  createRemoteSessionClientForm: CreateRemoteSessionClientForm;
};
/** @internal */
export type CreateRemoteSessionClientSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateRemoteSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionClientSecurityOption1$Outbound,
  CreateRemoteSessionClientSecurityOption1
>;
export declare function createRemoteSessionClientSecurityOption1ToJSON(
  createRemoteSessionClientSecurityOption1: CreateRemoteSessionClientSecurityOption1,
): string;
/** @internal */
export type CreateRemoteSessionClientSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateRemoteSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionClientSecurityOption2$Outbound,
  CreateRemoteSessionClientSecurityOption2
>;
export declare function createRemoteSessionClientSecurityOption2ToJSON(
  createRemoteSessionClientSecurityOption2: CreateRemoteSessionClientSecurityOption2,
): string;
/** @internal */
export type CreateRemoteSessionClientSecurity$Outbound = {
  Option1?: CreateRemoteSessionClientSecurityOption1$Outbound | undefined;
  Option2?: CreateRemoteSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionClientSecurity$Outbound,
  CreateRemoteSessionClientSecurity
>;
export declare function createRemoteSessionClientSecurityToJSON(
  createRemoteSessionClientSecurity: CreateRemoteSessionClientSecurity,
): string;
/** @internal */
export type CreateRemoteSessionClientRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateRemoteSessionClientForm: CreateRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const CreateRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionClientRequest$Outbound,
  CreateRemoteSessionClientRequest
>;
export declare function createRemoteSessionClientRequestToJSON(
  createRemoteSessionClientRequest: CreateRemoteSessionClientRequest,
): string;
//# sourceMappingURL=createremotesessionclient.d.ts.map
