import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Response filter metadata for the tool
 */
export type ResponseFilter = {
    /**
     * Content types to filter for
     */
    contentTypes: Array<string>;
    /**
     * Status codes to filter for
     */
    statusCodes: Array<string>;
    /**
     * Response filter type for the tool
     */
    type: string;
};
/** @internal */
export declare const ResponseFilter$inboundSchema: z.ZodMiniType<ResponseFilter, unknown>;
export declare function responseFilterFromJSON(jsonString: string): SafeParseResult<ResponseFilter, SDKValidationError>;
//# sourceMappingURL=responsefilter.d.ts.map