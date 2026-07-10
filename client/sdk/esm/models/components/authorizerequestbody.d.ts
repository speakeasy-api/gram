import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * PKCE challenge method. Only S256 is supported.
 */
export declare const CodeChallengeMethod: {
  readonly S256: "S256";
};
/**
 * PKCE challenge method. Only S256 is supported.
 */
export type CodeChallengeMethod = ClosedEnum<typeof CodeChallengeMethod>;
export type AuthorizeRequestBody = {
  /**
   * PKCE code challenge: base64url(sha256(code_verifier)).
   */
  codeChallenge: string;
  /**
   * PKCE challenge method. Only S256 is supported.
   */
  codeChallengeMethod: CodeChallengeMethod;
  /**
   * Optional project slug to scope the minted key to. Defaults to the org's default (first) project when omitted.
   */
  projectSlug?: string | undefined;
};
/** @internal */
export declare const CodeChallengeMethod$outboundSchema: z.ZodMiniEnum<
  typeof CodeChallengeMethod
>;
/** @internal */
export type AuthorizeRequestBody$Outbound = {
  code_challenge: string;
  code_challenge_method: string;
  project_slug?: string | undefined;
};
/** @internal */
export declare const AuthorizeRequestBody$outboundSchema: z.ZodMiniType<
  AuthorizeRequestBody$Outbound,
  AuthorizeRequestBody
>;
export declare function authorizeRequestBodyToJSON(
  authorizeRequestBody: AuthorizeRequestBody,
): string;
//# sourceMappingURL=authorizerequestbody.d.ts.map
