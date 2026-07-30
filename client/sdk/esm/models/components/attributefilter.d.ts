import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Comparison operator
 */
export declare const Op: {
  readonly Eq: "eq";
  readonly NotEq: "not_eq";
  readonly Contains: "contains";
  readonly Exists: "exists";
  readonly NotExists: "not_exists";
};
/**
 * Comparison operator
 */
export type Op = ClosedEnum<typeof Op>;
/**
 * Filter on a log attribute by path.
 */
export type AttributeFilter = {
  /**
   * Comparison operator
   */
  op?: Op | undefined;
  /**
   * Attribute path. Use @ prefix for custom attributes (e.g. '@user.region'), or bare path for system attributes (e.g. 'http.route').
   */
  path: string;
  /**
   * Value to compare against (ignored for 'exists' and 'not_exists' operators)
   */
  value?: string | undefined;
};
/** @internal */
export declare const Op$outboundSchema: z.ZodMiniEnum<typeof Op>;
/** @internal */
export type AttributeFilter$Outbound = {
  op: string;
  path: string;
  value?: string | undefined;
};
/** @internal */
export declare const AttributeFilter$outboundSchema: z.ZodMiniType<
  AttributeFilter$Outbound,
  AttributeFilter
>;
export declare function attributeFilterToJSON(
  attributeFilter: AttributeFilter,
): string;
//# sourceMappingURL=attributefilter.d.ts.map
