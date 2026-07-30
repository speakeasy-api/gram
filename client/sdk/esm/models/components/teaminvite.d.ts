import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const TeamInviteStatus: {
  readonly Pending: "pending";
  readonly Accepted: "accepted";
  readonly Expired: "expired";
  readonly Cancelled: "cancelled";
};
export type TeamInviteStatus = ClosedEnum<typeof TeamInviteStatus>;
export type TeamInvite = {
  /**
   * When the invite was created
   */
  createdAt: Date;
  /**
   * The invited email address
   */
  email: string;
  /**
   * When the invite expires
   */
  expiresAt: Date;
  /**
   * The invite ID
   */
  id: string;
  /**
   * Name of the user who sent the invite
   */
  invitedBy: string;
  status: TeamInviteStatus;
};
/** @internal */
export declare const TeamInviteStatus$inboundSchema: z.ZodMiniEnum<
  typeof TeamInviteStatus
>;
/** @internal */
export declare const TeamInvite$inboundSchema: z.ZodMiniType<
  TeamInvite,
  unknown
>;
export declare function teamInviteFromJSON(
  jsonString: string,
): SafeParseResult<TeamInvite, SDKValidationError>;
//# sourceMappingURL=teaminvite.d.ts.map
