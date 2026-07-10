import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListEnvironmentsResult } from "../models/components/listenvironmentsresult.js";
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
  ListEnvironmentsRequest,
  ListEnvironmentsSecurity,
} from "../models/operations/listenvironments.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listEnvironments environments
 *
 * @remarks
 * List all environments for an organization
 */
export declare function environmentsList(
  client: GramCore,
  request?: ListEnvironmentsRequest | undefined,
  security?: ListEnvironmentsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListEnvironmentsResult,
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
//# sourceMappingURL=environmentsList.d.ts.map
