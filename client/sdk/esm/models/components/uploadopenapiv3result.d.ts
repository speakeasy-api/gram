import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Asset } from "./asset.js";
export type UploadOpenAPIv3Result = {
    asset: Asset;
};
/** @internal */
export declare const UploadOpenAPIv3Result$inboundSchema: z.ZodMiniType<UploadOpenAPIv3Result, unknown>;
export declare function uploadOpenAPIv3ResultFromJSON(jsonString: string): SafeParseResult<UploadOpenAPIv3Result, SDKValidationError>;
//# sourceMappingURL=uploadopenapiv3result.d.ts.map