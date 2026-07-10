import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRolesResult } from "../models/components/listrolesresult.js";
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
  ListRolesRequest,
  ListRolesSecurity,
} from "../models/operations/listroles.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRoles access
 *
 * @remarks
 * List all roles for the current organization.
 */
export declare function accessListRoles(
  client: GramCore,
  request?: ListRolesRequest | undefined,
  security?: ListRolesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListRolesResult,
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
//# sourceMappingURL=accessListRoles.d.ts.map
