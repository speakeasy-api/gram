import * as z from "zod/v4-mini";
import {
  DeleteRequestBody,
  DeleteRequestBody$Outbound,
} from "../components/deleterequestbody.js";
export type DeleteServerNameOverrideSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteServerNameOverrideSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteServerNameOverrideSecurity = {
  option1?: DeleteServerNameOverrideSecurityOption1 | undefined;
  option2?: DeleteServerNameOverrideSecurityOption2 | undefined;
};
export type DeleteServerNameOverrideRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  deleteRequestBody: DeleteRequestBody;
};
/** @internal */
export type DeleteServerNameOverrideSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteServerNameOverrideSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteServerNameOverrideSecurityOption1$Outbound,
  DeleteServerNameOverrideSecurityOption1
>;
export declare function deleteServerNameOverrideSecurityOption1ToJSON(
  deleteServerNameOverrideSecurityOption1: DeleteServerNameOverrideSecurityOption1,
): string;
/** @internal */
export type DeleteServerNameOverrideSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteServerNameOverrideSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteServerNameOverrideSecurityOption2$Outbound,
  DeleteServerNameOverrideSecurityOption2
>;
export declare function deleteServerNameOverrideSecurityOption2ToJSON(
  deleteServerNameOverrideSecurityOption2: DeleteServerNameOverrideSecurityOption2,
): string;
/** @internal */
export type DeleteServerNameOverrideSecurity$Outbound = {
  Option1?: DeleteServerNameOverrideSecurityOption1$Outbound | undefined;
  Option2?: DeleteServerNameOverrideSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteServerNameOverrideSecurity$outboundSchema: z.ZodMiniType<
  DeleteServerNameOverrideSecurity$Outbound,
  DeleteServerNameOverrideSecurity
>;
export declare function deleteServerNameOverrideSecurityToJSON(
  deleteServerNameOverrideSecurity: DeleteServerNameOverrideSecurity,
): string;
/** @internal */
export type DeleteServerNameOverrideRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  DeleteRequestBody: DeleteRequestBody$Outbound;
};
/** @internal */
export declare const DeleteServerNameOverrideRequest$outboundSchema: z.ZodMiniType<
  DeleteServerNameOverrideRequest$Outbound,
  DeleteServerNameOverrideRequest
>;
export declare function deleteServerNameOverrideRequestToJSON(
  deleteServerNameOverrideRequest: DeleteServerNameOverrideRequest,
): string;
//# sourceMappingURL=deleteservernameoverride.d.ts.map
