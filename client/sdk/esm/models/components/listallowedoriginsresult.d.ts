import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AllowedOrigin } from "./allowedorigin.js";
export type ListAllowedOriginsResult = {
    /**
     * The list of allowed origins
     */
    allowedOrigins: Array<AllowedOrigin>;
};
/** @internal */
export declare const ListAllowedOriginsResult$inboundSchema: z.ZodMiniType<ListAllowedOriginsResult, unknown>;
export declare function listAllowedOriginsResultFromJSON(jsonString: string): SafeParseResult<ListAllowedOriginsResult, SDKValidationError>;
//# sourceMappingURL=listallowedoriginsresult.d.ts.map