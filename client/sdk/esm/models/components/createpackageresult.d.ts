import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Package } from "./package.js";
export type CreatePackageResult = {
    package: Package;
};
/** @internal */
export declare const CreatePackageResult$inboundSchema: z.ZodMiniType<CreatePackageResult, unknown>;
export declare function createPackageResultFromJSON(jsonString: string): SafeParseResult<CreatePackageResult, SDKValidationError>;
//# sourceMappingURL=createpackageresult.d.ts.map