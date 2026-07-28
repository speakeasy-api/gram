import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated usage summary for a role
 */
export type RoleSummary = {
  /**
   * Average cost per user (total_cost / user_count)
   */
  costPerUser: number;
  /**
   * Role identifier extracted from role URN
   */
  roleId: string;
  /**
   * Human-readable role name
   */
  roleName: string;
  /**
   * Total chat sessions across all users
   */
  totalChats: number;
  /**
   * Total cost across all users with this role
   */
  totalCost: number;
  /**
   * Sum of input tokens across all users
   */
  totalInputTokens: number;
  /**
   * Sum of output tokens across all users
   */
  totalOutputTokens: number;
  /**
   * Sum of all tokens across all users
   */
  totalTokens: number;
  /**
   * Number of users with this role
   */
  userCount: number;
};
/** @internal */
export declare const RoleSummary$inboundSchema: z.ZodMiniType<
  RoleSummary,
  unknown
>;
export declare function roleSummaryFromJSON(
  jsonString: string,
): SafeParseResult<RoleSummary, SDKValidationError>;
//# sourceMappingURL=rolesummary.d.ts.map
