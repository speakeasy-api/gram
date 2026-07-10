import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage user identity kind
 */
export declare const UserKind: {
  readonly Email: "email";
  readonly ExternalUserId: "external_user_id";
  readonly UserId: "user_id";
  readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type UserKind = ClosedEnum<typeof UserKind>;
/**
 * Tool usage user filter option with usage in the selected time window
 */
export type ToolUsageUserFilterOption = {
  /**
   * Number of tool usage events observed for the user identity
   */
  eventCount: number;
  /**
   * Stable user identity value used by filters
   */
  userKey: string;
  /**
   * Tool usage user identity kind
   */
  userKind: UserKind;
  /**
   * User-facing label for the user identity
   */
  userLabel: string;
};
/** @internal */
export declare const UserKind$inboundSchema: z.ZodMiniEnum<typeof UserKind>;
/** @internal */
export declare const ToolUsageUserFilterOption$inboundSchema: z.ZodMiniType<
  ToolUsageUserFilterOption,
  unknown
>;
export declare function toolUsageUserFilterOptionFromJSON(
  jsonString: string,
): SafeParseResult<ToolUsageUserFilterOption, SDKValidationError>;
//# sourceMappingURL=toolusageuserfilteroption.d.ts.map
