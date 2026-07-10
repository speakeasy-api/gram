import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TierLimits } from "./tierlimits.js";
export type UsageTiers = {
    enterprise: TierLimits;
    free: TierLimits;
    pro: TierLimits;
};
/** @internal */
export declare const UsageTiers$inboundSchema: z.ZodMiniType<UsageTiers, unknown>;
export declare function usageTiersFromJSON(jsonString: string): SafeParseResult<UsageTiers, SDKValidationError>;
//# sourceMappingURL=usagetiers.d.ts.map