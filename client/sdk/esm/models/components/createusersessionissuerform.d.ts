import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How multi-remote authn challenges are presented: chain | interactive.
 */
export declare const AuthnChallengeMode: {
  readonly Chain: "chain";
  readonly Interactive: "interactive";
};
/**
 * How multi-remote authn challenges are presented: chain | interactive.
 */
export type AuthnChallengeMode = ClosedEnum<typeof AuthnChallengeMode>;
/**
 * Form for creating a user_session_issuer.
 */
export type CreateUserSessionIssuerForm = {
  /**
   * How multi-remote authn challenges are presented: chain | interactive.
   */
  authnChallengeMode: AuthnChallengeMode;
  /**
   * Issued user session lifetime, in hours.
   */
  sessionDurationHours: number;
  /**
   * Project-unique slug.
   */
  slug: string;
};
/** @internal */
export declare const AuthnChallengeMode$outboundSchema: z.ZodMiniEnum<
  typeof AuthnChallengeMode
>;
/** @internal */
export type CreateUserSessionIssuerForm$Outbound = {
  authn_challenge_mode: string;
  session_duration_hours: number;
  slug: string;
};
/** @internal */
export declare const CreateUserSessionIssuerForm$outboundSchema: z.ZodMiniType<
  CreateUserSessionIssuerForm$Outbound,
  CreateUserSessionIssuerForm
>;
export declare function createUserSessionIssuerFormToJSON(
  createUserSessionIssuerForm: CreateUserSessionIssuerForm,
): string;
//# sourceMappingURL=createusersessionissuerform.d.ts.map
