import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Package } from "./package.js";
export type ListPackagesResult = {
    /**
     * The list of packages
     */
    packages: Array<Package>;
};
/** @internal */
export declare const ListPackagesResult$inboundSchema: z.ZodMiniType<ListPackagesResult, unknown>;
export declare function listPackagesResultFromJSON(jsonString: string): SafeParseResult<ListPackagesResult, SDKValidationError>;
//# sourceMappingURL=listpackagesresult.d.ts.map