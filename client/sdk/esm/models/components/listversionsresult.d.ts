import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Package } from "./package.js";
import { PackageVersion } from "./packageversion.js";
export type ListVersionsResult = {
    package: Package;
    versions: Array<PackageVersion>;
};
/** @internal */
export declare const ListVersionsResult$inboundSchema: z.ZodMiniType<ListVersionsResult, unknown>;
export declare function listVersionsResultFromJSON(jsonString: string): SafeParseResult<ListVersionsResult, SDKValidationError>;
//# sourceMappingURL=listversionsresult.d.ts.map