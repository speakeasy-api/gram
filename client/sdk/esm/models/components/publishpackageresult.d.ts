import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Package } from "./package.js";
import { PackageVersion } from "./packageversion.js";
export type PublishPackageResult = {
    package: Package;
    version: PackageVersion;
};
/** @internal */
export declare const PublishPackageResult$inboundSchema: z.ZodMiniType<PublishPackageResult, unknown>;
export declare function publishPackageResultFromJSON(jsonString: string): SafeParseResult<PublishPackageResult, SDKValidationError>;
//# sourceMappingURL=publishpackageresult.d.ts.map