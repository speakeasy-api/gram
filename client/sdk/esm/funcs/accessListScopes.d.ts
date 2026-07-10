import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListScopesResult } from "../models/components/listscopesresult.js";
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
  ListScopesRequest,
  ListScopesSecurity,
} from "../models/operations/listscopes.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listScopes access
 *
 * @remarks
 * List all available scopes and their resource types.
 */
export declare function accessListScopes(
  client: GramCore,
  request?: ListScopesRequest | undefined,
  security?: ListScopesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListScopesResult,
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
//# sourceMappingURL=accessListScopes.d.ts.map
