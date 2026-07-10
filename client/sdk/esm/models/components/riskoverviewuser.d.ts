import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskOverviewUser = {
  /**
   * User email, or Unknown user when unavailable.
   */
  email: string;
  /**
   * External user identifier as recorded on chats, when known. Empty when the finding cannot be attributed to an external user.
   */
  externalUserId: string;
  /**
   * Finding count for this user.
   */
  findings: number;
};
/** @internal */
export declare const RiskOverviewUser$inboundSchema: z.ZodMiniType<
  RiskOverviewUser,
  unknown
>;
export declare function riskOverviewUserFromJSON(
  jsonString: string,
): SafeParseResult<RiskOverviewUser, SDKValidationError>;
//# sourceMappingURL=riskoverviewuser.d.ts.map
