import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
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
  GetTriggerInstanceRequest,
  GetTriggerInstanceSecurity,
} from "../models/operations/gettriggerinstance.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getTriggerInstance triggers
 *
 * @remarks
 * Get a trigger instance by ID.
 */
export declare function triggersGet(
  client: GramCore,
  request: GetTriggerInstanceRequest,
  security?: GetTriggerInstanceSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    TriggerInstance,
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
//# sourceMappingURL=triggersGet.d.ts.map
