import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AuthorizeResponseBody } from "../models/components/authorizeresponsebody.js";
import { RedeemRequestBody } from "../models/components/redeemrequestbody.js";
import { RedeemResponseBody } from "../models/components/redeemresponsebody.js";
import {
  CliAuthAuthorizeRequest,
  CliAuthAuthorizeSecurity,
} from "../models/operations/cliauthauthorize.js";
export declare class CliAuth extends ClientSDK {
  /**
   * authorize cliAuth
   *
   * @remarks
   * Mint a short-lived one-time code bound to a PKCE code_challenge, on behalf of the authenticated dashboard user. Resolves the target project (given slug, else the org's default/first project) and records {user, org, project, scopes:[agent,hooks], challenge} against the code with a ~5 minute TTL. Requires a member-available session (org:read); NOT org-admin.
   */
  authorize(
    request: CliAuthAuthorizeRequest,
    security?: CliAuthAuthorizeSecurity | undefined,
    options?: RequestOptions,
  ): Promise<AuthorizeResponseBody>;
  /**
   * redeem cliAuth
   *
   * @remarks
   * Exchange a one-time code plus its PKCE code_verifier for a freshly minted per-user [agent,hooks] API key. No session or API-key auth: proving knowledge of the code_verifier that matches the stored challenge IS the credential. The code is single-use — consumed atomically on lookup — so any missing/expired/already-consumed code or PKCE mismatch returns 401. The raw key is returned exactly once and never again.
   */
  redeem(
    request: RedeemRequestBody,
    options?: RequestOptions,
  ): Promise<RedeemResponseBody>;
}
//# sourceMappingURL=cliauth.d.ts.map
