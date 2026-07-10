import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Asset } from "./asset.js";
export type UploadChatAttachmentResult = {
  asset: Asset;
  /**
   * The URL to serve the chat attachment
   */
  url: string;
};
/** @internal */
export declare const UploadChatAttachmentResult$inboundSchema: z.ZodMiniType<
  UploadChatAttachmentResult,
  unknown
>;
export declare function uploadChatAttachmentResultFromJSON(
  jsonString: string,
): SafeParseResult<UploadChatAttachmentResult, SDKValidationError>;
//# sourceMappingURL=uploadchatattachmentresult.d.ts.map
