import * as z from "zod/v4-mini";
import {
  AttachServerRequestBody,
  AttachServerRequestBody$Outbound,
} from "../components/attachserverrequestbody.js";
export type DetachServerFromCollectionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type DetachServerFromCollectionRequest = {
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
export type DetachServerFromCollectionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DetachServerFromCollectionSecurity$outboundSchema: z.ZodMiniType<
  DetachServerFromCollectionSecurity$Outbound,
  DetachServerFromCollectionSecurity
>;
export declare function detachServerFromCollectionSecurityToJSON(
  detachServerFromCollectionSecurity: DetachServerFromCollectionSecurity,
): string;
/** @internal */
export type DetachServerFromCollectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  AttachServerRequestBody: AttachServerRequestBody$Outbound;
};
/** @internal */
export declare const DetachServerFromCollectionRequest$outboundSchema: z.ZodMiniType<
  DetachServerFromCollectionRequest$Outbound,
  DetachServerFromCollectionRequest
>;
export declare function detachServerFromCollectionRequestToJSON(
  detachServerFromCollectionRequest: DetachServerFromCollectionRequest,
): string;
//# sourceMappingURL=detachserverfromcollection.d.ts.map
