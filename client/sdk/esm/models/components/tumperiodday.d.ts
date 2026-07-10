import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { RFCDate } from "../../types/rfcdate.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TUMPeriodDay = {
    /**
     * The UTC day
     */
    date: RFCDate;
    /**
     * Tokens under management consumed on this day
     */
    tokens: number;
};
/** @internal */
export declare const TUMPeriodDay$inboundSchema: z.ZodMiniType<TUMPeriodDay, unknown>;
export declare function tumPeriodDayFromJSON(jsonString: string): SafeParseResult<TUMPeriodDay, SDKValidationError>;
//# sourceMappingURL=tumperiodday.d.ts.map