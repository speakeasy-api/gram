import * as z from "zod/v4-mini";
import {
  DiscoverRemoteSessionIssuerRequestBody,
  DiscoverRemoteSessionIssuerRequestBody$Outbound,
} from "../components/discoverremotesessionissuerrequestbody.js";
export type DiscoverRemoteSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DiscoverRemoteSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DiscoverRemoteSessionIssuerSecurity = {
  option1?: DiscoverRemoteSessionIssuerSecurityOption1 | undefined;
  option2?: DiscoverRemoteSessionIssuerSecurityOption2 | undefined;
};
export type DiscoverRemoteSessionIssuerRequest = {
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
  discoverRemoteSessionIssuerRequestBody: DiscoverRemoteSessionIssuerRequestBody;
};
/** @internal */
export type DiscoverRemoteSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DiscoverRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  DiscoverRemoteSessionIssuerSecurityOption1$Outbound,
  DiscoverRemoteSessionIssuerSecurityOption1
>;
export declare function discoverRemoteSessionIssuerSecurityOption1ToJSON(
  discoverRemoteSessionIssuerSecurityOption1: DiscoverRemoteSessionIssuerSecurityOption1,
): string;
/** @internal */
export type DiscoverRemoteSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DiscoverRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  DiscoverRemoteSessionIssuerSecurityOption2$Outbound,
  DiscoverRemoteSessionIssuerSecurityOption2
>;
export declare function discoverRemoteSessionIssuerSecurityOption2ToJSON(
  discoverRemoteSessionIssuerSecurityOption2: DiscoverRemoteSessionIssuerSecurityOption2,
): string;
/** @internal */
export type DiscoverRemoteSessionIssuerSecurity$Outbound = {
  Option1?: DiscoverRemoteSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: DiscoverRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DiscoverRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  DiscoverRemoteSessionIssuerSecurity$Outbound,
  DiscoverRemoteSessionIssuerSecurity
>;
export declare function discoverRemoteSessionIssuerSecurityToJSON(
  discoverRemoteSessionIssuerSecurity: DiscoverRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type DiscoverRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  DiscoverRemoteSessionIssuerRequestBody: DiscoverRemoteSessionIssuerRequestBody$Outbound;
};
/** @internal */
export declare const DiscoverRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  DiscoverRemoteSessionIssuerRequest$Outbound,
  DiscoverRemoteSessionIssuerRequest
>;
export declare function discoverRemoteSessionIssuerRequestToJSON(
  discoverRemoteSessionIssuerRequest: DiscoverRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=discoverremotesessionissuer.d.ts.map
