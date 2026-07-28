import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServeChatAttachmentSignedRequest = {
  /**
   * The signed JWT token
   */
  token: string;
};
export type ServeChatAttachmentSignedResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type ServeChatAttachmentSignedRequest$Outbound = {
  token: string;
};
/** @internal */
export declare const ServeChatAttachmentSignedRequest$outboundSchema: z.ZodMiniType<
  ServeChatAttachmentSignedRequest$Outbound,
  ServeChatAttachmentSignedRequest
>;
export declare function serveChatAttachmentSignedRequestToJSON(
  serveChatAttachmentSignedRequest: ServeChatAttachmentSignedRequest,
): string;
/** @internal */
export declare const ServeChatAttachmentSignedResponse$inboundSchema: z.ZodMiniType<
  ServeChatAttachmentSignedResponse,
  unknown
>;
export declare function serveChatAttachmentSignedResponseFromJSON(
  jsonString: string,
): SafeParseResult<ServeChatAttachmentSignedResponse, SDKValidationError>;
//# sourceMappingURL=servechatattachmentsigned.d.ts.map
