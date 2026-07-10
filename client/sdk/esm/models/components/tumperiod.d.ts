import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TUMPeriodDay } from "./tumperiodday.js";
export type TUMPeriod = {
    /**
     * Daily breakdown of TUM within the cycle. Days without usage are omitted.
     */
    days: Array<TUMPeriodDay>;
    /**
     * End of the billing cycle (exclusive)
     */
    periodEnd: Date;
    /**
     * Start of the billing cycle
     */
    periodStart: Date;
    /**
     * Tokens under management consumed during the cycle
     */
    tokens: number;
};
/** @internal */
export declare const TUMPeriod$inboundSchema: z.ZodMiniType<TUMPeriod, unknown>;
export declare function tumPeriodFromJSON(jsonString: string): SafeParseResult<TUMPeriod, SDKValidationError>;
//# sourceMappingURL=tumperiod.d.ts.map