import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { LogFilter, LogFilter$Outbound } from "./logfilter.js";
export declare const TypesToInclude: {
    readonly Mcp: "mcp";
    readonly Local: "local";
    readonly Skill: "skill";
};
export type TypesToInclude = ClosedEnum<typeof TypesToInclude>;
/**
 * Payload for getting aggregated hooks metrics
 */
export type GetHooksSummaryPayload = {
    /**
     * Filter conditions (same as listHooksTraces)
     */
    filters?: Array<LogFilter> | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
    /**
     * Hook types to include (mcp, local, skill). If empty, includes all types.
     */
    typesToInclude?: Array<TypesToInclude> | undefined;
};
/** @internal */
export declare const TypesToInclude$outboundSchema: z.ZodMiniEnum<typeof TypesToInclude>;
/** @internal */
export type GetHooksSummaryPayload$Outbound = {
    filters?: Array<LogFilter$Outbound> | undefined;
    from: string;
    to: string;
    types_to_include?: Array<string> | undefined;
};
/** @internal */
export declare const GetHooksSummaryPayload$outboundSchema: z.ZodMiniType<GetHooksSummaryPayload$Outbound, GetHooksSummaryPayload>;
export declare function getHooksSummaryPayloadToJSON(getHooksSummaryPayload: GetHooksSummaryPayload): string;
//# sourceMappingURL=gethookssummarypayload.d.ts.map