import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Environment } from "./environment.js";
/**
 * Result type for listing environments
 */
export type ListEnvironmentsResult = {
    environments: Array<Environment>;
};
/** @internal */
export declare const ListEnvironmentsResult$inboundSchema: z.ZodMiniType<ListEnvironmentsResult, unknown>;
export declare function listEnvironmentsResultFromJSON(jsonString: string): SafeParseResult<ListEnvironmentsResult, SDKValidationError>;
//# sourceMappingURL=listenvironmentsresult.d.ts.map