import * as z from "zod/v4-mini";
import {
  CreateRemoteSessionIssuerForm,
  CreateRemoteSessionIssuerForm$Outbound,
} from "../components/createremotesessionissuerform.js";
export type CreateRemoteSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateRemoteSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateRemoteSessionIssuerSecurity = {
  option1?: CreateRemoteSessionIssuerSecurityOption1 | undefined;
  option2?: CreateRemoteSessionIssuerSecurityOption2 | undefined;
};
export type CreateRemoteSessionIssuerRequest = {
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
  createRemoteSessionIssuerForm: CreateRemoteSessionIssuerForm;
};
/** @internal */
export type CreateRemoteSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionIssuerSecurityOption1$Outbound,
  CreateRemoteSessionIssuerSecurityOption1
>;
export declare function createRemoteSessionIssuerSecurityOption1ToJSON(
  createRemoteSessionIssuerSecurityOption1: CreateRemoteSessionIssuerSecurityOption1,
): string;
/** @internal */
export type CreateRemoteSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionIssuerSecurityOption2$Outbound,
  CreateRemoteSessionIssuerSecurityOption2
>;
export declare function createRemoteSessionIssuerSecurityOption2ToJSON(
  createRemoteSessionIssuerSecurityOption2: CreateRemoteSessionIssuerSecurityOption2,
): string;
/** @internal */
export type CreateRemoteSessionIssuerSecurity$Outbound = {
  Option1?: CreateRemoteSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: CreateRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionIssuerSecurity$Outbound,
  CreateRemoteSessionIssuerSecurity
>;
export declare function createRemoteSessionIssuerSecurityToJSON(
  createRemoteSessionIssuerSecurity: CreateRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type CreateRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateRemoteSessionIssuerForm: CreateRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const CreateRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  CreateRemoteSessionIssuerRequest$Outbound,
  CreateRemoteSessionIssuerRequest
>;
export declare function createRemoteSessionIssuerRequestToJSON(
  createRemoteSessionIssuerRequest: CreateRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=createremotesessionissuer.d.ts.map
