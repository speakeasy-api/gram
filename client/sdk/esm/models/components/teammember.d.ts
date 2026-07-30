import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TeamMember = {
  /**
   * The user's display name
   */
  displayName: string;
  /**
   * The user's email address
   */
  email: string;
  /**
   * The user's ID
   */
  id: string;
  /**
   * When the user joined the organization
   */
  joinedAt: Date;
  /**
   * URL to the user's profile photo
   */
  photoUrl?: string | undefined;
};
/** @internal */
export declare const TeamMember$inboundSchema: z.ZodMiniType<
  TeamMember,
  unknown
>;
export declare function teamMemberFromJSON(
  jsonString: string,
): SafeParseResult<TeamMember, SDKValidationError>;
//# sourceMappingURL=teammember.d.ts.map
