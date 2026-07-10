import * as z from "zod/v4-mini";
import {
  SetBillingMetadataRequestBody,
  SetBillingMetadataRequestBody$Outbound,
} from "../components/setbillingmetadatarequestbody.js";
export type SetBillingMetadataSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type SetBillingMetadataRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  setBillingMetadataRequestBody: SetBillingMetadataRequestBody;
};
/** @internal */
export type SetBillingMetadataSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetBillingMetadataSecurity$outboundSchema: z.ZodMiniType<
  SetBillingMetadataSecurity$Outbound,
  SetBillingMetadataSecurity
>;
export declare function setBillingMetadataSecurityToJSON(
  setBillingMetadataSecurity: SetBillingMetadataSecurity,
): string;
/** @internal */
export type SetBillingMetadataRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  SetBillingMetadataRequestBody: SetBillingMetadataRequestBody$Outbound;
};
/** @internal */
export declare const SetBillingMetadataRequest$outboundSchema: z.ZodMiniType<
  SetBillingMetadataRequest$Outbound,
  SetBillingMetadataRequest
>;
export declare function setBillingMetadataRequestToJSON(
  setBillingMetadataRequest: SetBillingMetadataRequest,
): string;
//# sourceMappingURL=setbillingmetadata.d.ts.map
