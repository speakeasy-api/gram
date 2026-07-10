import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  SwitchAuthScopesRequest,
  SwitchAuthScopesResponse,
  SwitchAuthScopesSecurity,
} from "../models/operations/switchauthscopes.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * switchScopes auth
 *
 * @remarks
 * Switches the authentication scope to a different organization.
 */
export declare function authSwitchScopes(
  client: GramCore,
  request?: SwitchAuthScopesRequest | undefined,
  security?: SwitchAuthScopesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SwitchAuthScopesResponse | undefined,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=authSwitchScopes.d.ts.map
