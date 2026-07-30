import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result type for unread notification count
 */
export type UnreadCountResult = {
  /**
   * The number of unread notifications
   */
  count: number;
};
/** @internal */
export declare const UnreadCountResult$inboundSchema: z.ZodType<
  UnreadCountResult,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type UnreadCountResult$Outbound = {
  count: number;
};
/** @internal */
export declare const UnreadCountResult$outboundSchema: z.ZodType<
  UnreadCountResult$Outbound,
  z.ZodTypeDef,
  UnreadCountResult
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace UnreadCountResult$ {
  /** @deprecated use `UnreadCountResult$inboundSchema` instead. */
  const inboundSchema: z.ZodType<UnreadCountResult, z.ZodTypeDef, unknown>;
  /** @deprecated use `UnreadCountResult$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    UnreadCountResult$Outbound,
    z.ZodTypeDef,
    UnreadCountResult
  >;
  /** @deprecated use `UnreadCountResult$Outbound` instead. */
  type Outbound = UnreadCountResult$Outbound;
}
export declare function unreadCountResultToJSON(
  unreadCountResult: UnreadCountResult,
): string;
export declare function unreadCountResultFromJSON(
  jsonString: string,
): SafeParseResult<UnreadCountResult, SDKValidationError>;
//# sourceMappingURL=unreadcountresult.d.ts.map
