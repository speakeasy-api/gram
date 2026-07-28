import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * chain | interactive.
 */
export declare const UpdateUserSessionIssuerFormAuthnChallengeMode: {
  readonly Chain: "chain";
  readonly Interactive: "interactive";
};
/**
 * chain | interactive.
 */
export type UpdateUserSessionIssuerFormAuthnChallengeMode = ClosedEnum<
  typeof UpdateUserSessionIssuerFormAuthnChallengeMode
>;
/**
 * Form for updating a user_session_issuer. All non-id fields are optional patches.
 */
export type UpdateUserSessionIssuerForm = {
  /**
   * chain | interactive.
   */
  authnChallengeMode?:
    | UpdateUserSessionIssuerFormAuthnChallengeMode
    | undefined;
  /**
   * The user_session_issuer id.
   */
  id: string;
  /**
   * Issued user session lifetime, in hours.
   */
  sessionDurationHours?: number | undefined;
  /**
   * Rename the slug.
   */
  slug?: string | undefined;
};
/** @internal */
export declare const UpdateUserSessionIssuerFormAuthnChallengeMode$outboundSchema: z.ZodMiniEnum<
  typeof UpdateUserSessionIssuerFormAuthnChallengeMode
>;
/** @internal */
export type UpdateUserSessionIssuerForm$Outbound = {
  authn_challenge_mode?: string | undefined;
  id: string;
  session_duration_hours?: number | undefined;
  slug?: string | undefined;
};
/** @internal */
export declare const UpdateUserSessionIssuerForm$outboundSchema: z.ZodMiniType<
  UpdateUserSessionIssuerForm$Outbound,
  UpdateUserSessionIssuerForm
>;
export declare function updateUserSessionIssuerFormToJSON(
  updateUserSessionIssuerForm: UpdateUserSessionIssuerForm,
): string;
//# sourceMappingURL=updateusersessionissuerform.d.ts.map
