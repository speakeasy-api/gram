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
  DeleteServerNameOverrideRequest,
  DeleteServerNameOverrideSecurity,
} from "../models/operations/deleteservernameoverride.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * delete hooksServerNames
 *
 * @remarks
 * Delete a server name display override
 */
export declare function hooksServerNamesDeleteServerNameOverride(
  client: GramCore,
  request: DeleteServerNameOverrideRequest,
  security?: DeleteServerNameOverrideSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=hooksServerNamesDeleteServerNameOverride.d.ts.map
