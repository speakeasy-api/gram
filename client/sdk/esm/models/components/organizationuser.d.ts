import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type OrganizationUser = {
  createdAt: Date;
  /**
   * User email address.
   */
  email: string;
  /**
   * Gram relationship row ID.
   */
  id: string;
  /**
   * Timestamp of the user's most recent login.
   */
  lastLogin?: Date | undefined;
  /**
   * User display name.
   */
  name: string;
  /**
   * Gram organization ID.
   */
  organizationId: string;
  /**
   * User photo URL.
   */
  photoUrl?: string | undefined;
  updatedAt: Date;
  /**
   * Gram user ID.
   */
  userId: string;
  /**
   * WorkOS organization membership ID when known.
   */
  workosMembershipId?: string | undefined;
};
/** @internal */
export declare const OrganizationUser$inboundSchema: z.ZodMiniType<
  OrganizationUser,
  unknown
>;
export declare function organizationUserFromJSON(
  jsonString: string,
): SafeParseResult<OrganizationUser, SDKValidationError>;
//# sourceMappingURL=organizationuser.d.ts.map
