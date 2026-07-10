import * as z from "zod/v4-mini";
/**
 * Token and cost usage payload.
 */
export type HookUsageData = {
    /**
     * Cache read token count.
     */
    cacheReadTokens?: number | undefined;
    /**
     * Cache write token count.
     */
    cacheWriteTokens?: number | undefined;
    /**
     * Reported cost.
     */
    cost?: number | undefined;
    /**
     * Input token count.
     */
    inputTokens?: number | undefined;
    /**
     * Agent loop count, when reported.
     */
    loopCount?: number | undefined;
    /**
     * Output token count.
     */
    outputTokens?: number | undefined;
    /**
     * Provider-reported usage or session status, when available.
     */
    status?: string | undefined;
};
/** @internal */
export type HookUsageData$Outbound = {
    cache_read_tokens?: number | undefined;
    cache_write_tokens?: number | undefined;
    cost?: number | undefined;
    input_tokens?: number | undefined;
    loop_count?: number | undefined;
    output_tokens?: number | undefined;
    status?: string | undefined;
};
/** @internal */
export declare const HookUsageData$outboundSchema: z.ZodMiniType<HookUsageData$Outbound, HookUsageData>;
export declare function hookUsageDataToJSON(hookUsageData: HookUsageData): string;
//# sourceMappingURL=hookusagedata.d.ts.map