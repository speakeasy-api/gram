import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Comparison operator
 */
export declare const Operator: {
    readonly Eq: "eq";
    readonly NotEq: "not_eq";
    readonly Contains: "contains";
    readonly Exists: "exists";
    readonly NotExists: "not_exists";
    readonly In: "in";
};
/**
 * Comparison operator
 */
export type Operator = ClosedEnum<typeof Operator>;
/**
 * A single filter condition for a log search query.
 */
export type LogFilter = {
    /**
     * Comparison operator
     */
    operator?: Operator | undefined;
    /**
     * Attribute path. Use @ prefix for custom attributes (e.g. '@user.region'), or bare path for system attributes (e.g. 'http.route').
     */
    path: string;
    /**
     * Values to compare against. Pass one value for single-value operators (eq, not_eq, contains) and multiple for 'in'. Ignored for 'exists' and 'not_exists'.
     */
    values?: Array<string> | undefined;
};
/** @internal */
export declare const Operator$outboundSchema: z.ZodMiniEnum<typeof Operator>;
/** @internal */
export type LogFilter$Outbound = {
    operator: string;
    path: string;
    values?: Array<string> | undefined;
};
/** @internal */
export declare const LogFilter$outboundSchema: z.ZodMiniType<LogFilter$Outbound, LogFilter>;
export declare function logFilterToJSON(logFilter: LogFilter): string;
//# sourceMappingURL=logfilter.d.ts.map