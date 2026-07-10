import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AllowedOrigin } from "./allowedorigin.js";
export type UpsertAllowedOriginResult = {
    allowedOrigin: AllowedOrigin;
};
/** @internal */
export declare const UpsertAllowedOriginResult$inboundSchema: z.ZodMiniType<UpsertAllowedOriginResult, unknown>;
export declare function upsertAllowedOriginResultFromJSON(jsonString: string): SafeParseResult<UpsertAllowedOriginResult, SDKValidationError>;
//# sourceMappingURL=upsertallowedoriginresult.d.ts.map