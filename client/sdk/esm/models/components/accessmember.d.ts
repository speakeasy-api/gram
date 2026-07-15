import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AccessMember = {
  /**
   * Email address.
   */
  email: string;
  /**
   * User ID.
   */
  id: string;
  /**
   * When the member joined the organization.
   */
  joinedAt: Date;
  /**
   * Display name.
   */
  name: string;
  /**
   * Avatar URL.
   */
  photoUrl?: string | undefined;
  /**
   * Canonical principal URN for this member.
   */
  principalUrn: string;
  /**
   * All role IDs assigned to this member.
   */
  roleIds: Array<string>;
};
/** @internal */
export declare const AccessMember$inboundSchema: z.ZodMiniType<
  AccessMember,
  unknown
>;
export declare function accessMemberFromJSON(
  jsonString: string,
): SafeParseResult<AccessMember, SDKValidationError>;
//# sourceMappingURL=accessmember.d.ts.map
