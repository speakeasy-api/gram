import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Asset } from "./asset.js";
export type UploadImageResult = {
    asset: Asset;
};
/** @internal */
export declare const UploadImageResult$inboundSchema: z.ZodMiniType<UploadImageResult, unknown>;
export declare function uploadImageResultFromJSON(jsonString: string): SafeParseResult<UploadImageResult, SDKValidationError>;
//# sourceMappingURL=uploadimageresult.d.ts.map