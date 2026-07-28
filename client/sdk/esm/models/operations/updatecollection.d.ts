import * as z from "zod/v4-mini";
import {
  UpdateRequestBody,
  UpdateRequestBody$Outbound,
} from "../components/updaterequestbody.js";
export type UpdateCollectionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type UpdateCollectionRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  updateRequestBody: UpdateRequestBody;
};
/** @internal */
export type UpdateCollectionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const UpdateCollectionSecurity$outboundSchema: z.ZodMiniType<
  UpdateCollectionSecurity$Outbound,
  UpdateCollectionSecurity
>;
export declare function updateCollectionSecurityToJSON(
  updateCollectionSecurity: UpdateCollectionSecurity,
): string;
/** @internal */
export type UpdateCollectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  UpdateRequestBody: UpdateRequestBody$Outbound;
};
/** @internal */
export declare const UpdateCollectionRequest$outboundSchema: z.ZodMiniType<
  UpdateCollectionRequest$Outbound,
  UpdateCollectionRequest
>;
export declare function updateCollectionRequestToJSON(
  updateCollectionRequest: UpdateCollectionRequest,
): string;
//# sourceMappingURL=updatecollection.d.ts.map
