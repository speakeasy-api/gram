import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A user_session_issuer record.
 */
export type UserSessionIssuer = {
  /**
   * chain | interactive.
   */
  authnChallengeMode: string;
  createdAt: Date;
  /**
   * The user_session_issuer id.
   */
  id: string;
  /**
   * The owning project id.
   */
  projectId: string;
  /**
   * Issued user session lifetime, in hours.
   */
  sessionDurationHours: number;
  /**
   * Project-unique slug.
   */
  slug: string;
  updatedAt: Date;
};
/** @internal */
export declare const UserSessionIssuer$inboundSchema: z.ZodMiniType<
  UserSessionIssuer,
  unknown
>;
export declare function userSessionIssuerFromJSON(
  jsonString: string,
): SafeParseResult<UserSessionIssuer, SDKValidationError>;
//# sourceMappingURL=usersessionissuer.d.ts.map
