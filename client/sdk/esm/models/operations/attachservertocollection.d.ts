import * as z from "zod/v4-mini";
import {
  AttachServerRequestBody,
  AttachServerRequestBody$Outbound,
} from "../components/attachserverrequestbody.js";
export type AttachServerToCollectionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type AttachServerToCollectionRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  attachServerRequestBody: AttachServerRequestBody;
};
/** @internal */
export type AttachServerToCollectionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const AttachServerToCollectionSecurity$outboundSchema: z.ZodMiniType<
  AttachServerToCollectionSecurity$Outbound,
  AttachServerToCollectionSecurity
>;
export declare function attachServerToCollectionSecurityToJSON(
  attachServerToCollectionSecurity: AttachServerToCollectionSecurity,
): string;
/** @internal */
export type AttachServerToCollectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  AttachServerRequestBody: AttachServerRequestBody$Outbound;
};
/** @internal */
export declare const AttachServerToCollectionRequest$outboundSchema: z.ZodMiniType<
  AttachServerToCollectionRequest$Outbound,
  AttachServerToCollectionRequest
>;
export declare function attachServerToCollectionRequestToJSON(
  attachServerToCollectionRequest: AttachServerToCollectionRequest,
): string;
//# sourceMappingURL=attachservertocollection.d.ts.map
