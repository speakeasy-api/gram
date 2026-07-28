import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUserGrantsResult } from "../models/components/listusergrantsresult.js";
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
  ListGrantsRequest,
  ListGrantsSecurity,
} from "../models/operations/listgrants.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listGrants access
 *
 * @remarks
 * List the current user's effective grants, including inherited role grants.
 */
export declare function accessListGrants(
  client: GramCore,
  request?: ListGrantsRequest | undefined,
  security?: ListGrantsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListUserGrantsResult,
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
//# sourceMappingURL=accessListGrants.d.ts.map
