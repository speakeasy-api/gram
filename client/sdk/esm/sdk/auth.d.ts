import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import {
  AuthCallbackRequest,
  AuthCallbackResponse,
} from "../models/operations/authcallback.js";
import {
  AuthLoginRequest,
  AuthLoginResponse,
} from "../models/operations/authlogin.js";
import {
  LogoutRequest,
  LogoutResponse,
  LogoutSecurity,
} from "../models/operations/logout.js";
import {
  RegisterRequest,
  RegisterSecurity,
} from "../models/operations/register.js";
import {
  SessionInfoRequest,
  SessionInfoResponse,
  SessionInfoSecurity,
} from "../models/operations/sessioninfo.js";
import {
  SwitchAuthScopesRequest,
  SwitchAuthScopesResponse,
  SwitchAuthScopesSecurity,
} from "../models/operations/switchauthscopes.js";
export declare class Auth extends ClientSDK {
  /**
   * callback auth
   *
   * @remarks
   * Handles the authentication callback.
   */
  callback(
    request: AuthCallbackRequest,
    options?: RequestOptions,
  ): Promise<AuthCallbackResponse | undefined>;
  /**
   * info auth
   *
   * @remarks
   * Provides information about the current authentication status.
   */
  info(
    request?: SessionInfoRequest | undefined,
    security?: SessionInfoSecurity | undefined,
    options?: RequestOptions,
  ): Promise<SessionInfoResponse>;
  /**
   * login auth
   *
   * @remarks
   * Proxies to auth login through speakeasy oidc.
   */
  login(
    request?: AuthLoginRequest | undefined,
    options?: RequestOptions,
  ): Promise<AuthLoginResponse | undefined>;
  /**
   * logout auth
   *
   * @remarks
   * Logs out the current user by clearing their session.
   */
  logout(
    request?: LogoutRequest | undefined,
    security?: LogoutSecurity | undefined,
    options?: RequestOptions,
  ): Promise<LogoutResponse | undefined>;
  /**
   * register auth
   *
   * @remarks
   * Register a new org for a user with their session information.
   */
  register(
    request: RegisterRequest,
    security?: RegisterSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * switchScopes auth
   *
   * @remarks
   * Switches the authentication scope to a different organization.
   */
  switchScopes(
    request?: SwitchAuthScopesRequest | undefined,
    security?: SwitchAuthScopesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<SwitchAuthScopesResponse | undefined>;
}
//# sourceMappingURL=auth.d.ts.map
