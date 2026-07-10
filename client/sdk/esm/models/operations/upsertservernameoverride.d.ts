import * as z from "zod/v4-mini";
import {
  UpsertRequestBody,
  UpsertRequestBody$Outbound,
} from "../components/upsertrequestbody.js";
export type UpsertServerNameOverrideSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpsertServerNameOverrideSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpsertServerNameOverrideSecurity = {
  option1?: UpsertServerNameOverrideSecurityOption1 | undefined;
  option2?: UpsertServerNameOverrideSecurityOption2 | undefined;
};
export type UpsertServerNameOverrideRequest = {
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
  upsertRequestBody: UpsertRequestBody;
};
/** @internal */
export type UpsertServerNameOverrideSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpsertServerNameOverrideSecurityOption1$outboundSchema: z.ZodMiniType<
  UpsertServerNameOverrideSecurityOption1$Outbound,
  UpsertServerNameOverrideSecurityOption1
>;
export declare function upsertServerNameOverrideSecurityOption1ToJSON(
  upsertServerNameOverrideSecurityOption1: UpsertServerNameOverrideSecurityOption1,
): string;
/** @internal */
export type UpsertServerNameOverrideSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpsertServerNameOverrideSecurityOption2$outboundSchema: z.ZodMiniType<
  UpsertServerNameOverrideSecurityOption2$Outbound,
  UpsertServerNameOverrideSecurityOption2
>;
export declare function upsertServerNameOverrideSecurityOption2ToJSON(
  upsertServerNameOverrideSecurityOption2: UpsertServerNameOverrideSecurityOption2,
): string;
/** @internal */
export type UpsertServerNameOverrideSecurity$Outbound = {
  Option1?: UpsertServerNameOverrideSecurityOption1$Outbound | undefined;
  Option2?: UpsertServerNameOverrideSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpsertServerNameOverrideSecurity$outboundSchema: z.ZodMiniType<
  UpsertServerNameOverrideSecurity$Outbound,
  UpsertServerNameOverrideSecurity
>;
export declare function upsertServerNameOverrideSecurityToJSON(
  upsertServerNameOverrideSecurity: UpsertServerNameOverrideSecurity,
): string;
/** @internal */
export type UpsertServerNameOverrideRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpsertRequestBody: UpsertRequestBody$Outbound;
};
/** @internal */
export declare const UpsertServerNameOverrideRequest$outboundSchema: z.ZodMiniType<
  UpsertServerNameOverrideRequest$Outbound,
  UpsertServerNameOverrideRequest
>;
export declare function upsertServerNameOverrideRequestToJSON(
  upsertServerNameOverrideRequest: UpsertServerNameOverrideRequest,
): string;
//# sourceMappingURL=upsertservernameoverride.d.ts.map
