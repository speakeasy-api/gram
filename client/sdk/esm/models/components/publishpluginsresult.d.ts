import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PublishPluginsResult = {
  /**
   * The URL of the published GitHub repository.
   */
  repoUrl: string;
};
/** @internal */
export declare const PublishPluginsResult$inboundSchema: z.ZodMiniType<
  PublishPluginsResult,
  unknown
>;
export declare function publishPluginsResultFromJSON(
  jsonString: string,
): SafeParseResult<PublishPluginsResult, SDKValidationError>;
//# sourceMappingURL=publishpluginsresult.d.ts.map
