import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetInstanceResult } from "../models/components/getinstanceresult.js";
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
  GetInstanceRequest,
  GetInstanceSecurity,
} from "../models/operations/getinstance.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getInstance instances
 *
 * @remarks
 * Load all relevant data for an instance of a toolset and environment
 */
export declare function instancesGetBySlug(
  client: GramCore,
  request: GetInstanceRequest,
  security?: GetInstanceSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetInstanceResult,
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
//# sourceMappingURL=instancesGetBySlug.d.ts.map
