import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Tool usage user identity kind
 */
export declare const ToolUsageUserFilterKind: {
  readonly Email: "email";
  readonly ExternalUserId: "external_user_id";
  readonly UserId: "user_id";
  readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type ToolUsageUserFilterKind = ClosedEnum<
  typeof ToolUsageUserFilterKind
>;
/**
 * Typed user identity filter
 */
export type ToolUsageUserFilter = {
  /**
   * User identity value to include
   */
  key: string;
  /**
   * Tool usage user identity kind
   */
  kind: ToolUsageUserFilterKind;
};
/** @internal */
export declare const ToolUsageUserFilterKind$outboundSchema: z.ZodMiniEnum<
  typeof ToolUsageUserFilterKind
>;
/** @internal */
export type ToolUsageUserFilter$Outbound = {
  key: string;
  kind: string;
};
/** @internal */
export declare const ToolUsageUserFilter$outboundSchema: z.ZodMiniType<
  ToolUsageUserFilter$Outbound,
  ToolUsageUserFilter
>;
export declare function toolUsageUserFilterToJSON(
  toolUsageUserFilter: ToolUsageUserFilter,
): string;
//# sourceMappingURL=toolusageuserfilter.d.ts.map
