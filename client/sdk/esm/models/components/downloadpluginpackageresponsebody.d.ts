import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DownloadPluginPackageResponseBody = {
  /**
   * ZIP archive contents.
   */
  body: Uint8Array | string;
};
/** @internal */
export declare const DownloadPluginPackageResponseBody$inboundSchema: z.ZodMiniType<
  DownloadPluginPackageResponseBody,
  unknown
>;
export declare function downloadPluginPackageResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<DownloadPluginPackageResponseBody, SDKValidationError>;
//# sourceMappingURL=downloadpluginpackageresponsebody.d.ts.map
