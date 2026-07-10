import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServeImageRequest = {
  /**
   * The ID of the asset to serve
   */
  id: string;
};
export type ServeImageResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type ServeImageRequest$Outbound = {
  id: string;
};
/** @internal */
export declare const ServeImageRequest$outboundSchema: z.ZodMiniType<
  ServeImageRequest$Outbound,
  ServeImageRequest
>;
export declare function serveImageRequestToJSON(
  serveImageRequest: ServeImageRequest,
): string;
/** @internal */
export declare const ServeImageResponse$inboundSchema: z.ZodMiniType<
  ServeImageResponse,
  unknown
>;
export declare function serveImageResponseFromJSON(
  jsonString: string,
): SafeParseResult<ServeImageResponse, SDKValidationError>;
//# sourceMappingURL=serveimage.d.ts.map
