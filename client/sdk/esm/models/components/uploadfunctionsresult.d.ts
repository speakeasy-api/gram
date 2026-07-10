import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Asset } from "./asset.js";
export type UploadFunctionsResult = {
  asset: Asset;
};
/** @internal */
export declare const UploadFunctionsResult$inboundSchema: z.ZodMiniType<
  UploadFunctionsResult,
  unknown
>;
export declare function uploadFunctionsResultFromJSON(
  jsonString: string,
): SafeParseResult<UploadFunctionsResult, SDKValidationError>;
//# sourceMappingURL=uploadfunctionsresult.d.ts.map
