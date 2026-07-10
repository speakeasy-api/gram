import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PeriodUsage = {
    /**
     * The number of servers enabled at the time of the request
     */
    actualEnabledServerCount: number;
    /**
     * The number of credits used
     */
    credits: number;
    /**
     * Whether the project has an active subscription
     */
    hasActiveSubscription: boolean;
    /**
     * The number of credits included in the tier
     */
    includedCredits: number;
    /**
     * The number of servers included in the tier
     */
    includedServers: number;
    /**
     * The number of tool calls included in the tier
     */
    includedToolCalls: number;
    /**
     * The number of servers used, according to the Polar meter
     */
    servers: number;
    /**
     * The number of tool calls used
     */
    toolCalls: number;
};
/** @internal */
export declare const PeriodUsage$inboundSchema: z.ZodMiniType<PeriodUsage, unknown>;
export declare function periodUsageFromJSON(jsonString: string): SafeParseResult<PeriodUsage, SDKValidationError>;
//# sourceMappingURL=periodusage.d.ts.map