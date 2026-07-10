import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Package } from "./package.js";
export type UpdatePackageResult = {
    package: Package;
};
/** @internal */
export declare const UpdatePackageResult$inboundSchema: z.ZodMiniType<UpdatePackageResult, unknown>;
export declare function updatePackageResultFromJSON(jsonString: string): SafeParseResult<UpdatePackageResult, SDKValidationError>;
//# sourceMappingURL=updatepackageresult.d.ts.map