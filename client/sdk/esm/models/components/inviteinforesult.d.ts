import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Current status of the invite
 */
export declare const InviteInfoResultStatus: {
  readonly Pending: "pending";
  readonly Accepted: "accepted";
  readonly Expired: "expired";
  readonly Cancelled: "cancelled";
};
/**
 * Current status of the invite
 */
export type InviteInfoResultStatus = ClosedEnum<typeof InviteInfoResultStatus>;
export type InviteInfoResult = {
  /**
   * The email address the invite was sent to
   */
  email: string;
  /**
   * Display name of the user who sent the invite
   */
  inviterName: string;
  /**
   * Name of the organization
   */
  organizationName: string;
  /**
   * Current status of the invite
   */
  status: InviteInfoResultStatus;
};
/** @internal */
export declare const InviteInfoResultStatus$inboundSchema: z.ZodMiniEnum<
  typeof InviteInfoResultStatus
>;
/** @internal */
export declare const InviteInfoResult$inboundSchema: z.ZodMiniType<
  InviteInfoResult,
  unknown
>;
export declare function inviteInfoResultFromJSON(
  jsonString: string,
): SafeParseResult<InviteInfoResult, SDKValidationError>;
//# sourceMappingURL=inviteinforesult.d.ts.map
