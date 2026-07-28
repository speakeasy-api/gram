import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
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
  GetSourceEnvironmentRequest,
  GetSourceEnvironmentSecurity,
} from "../models/operations/getsourceenvironment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getSourceEnvironment environments
 *
 * @remarks
 * Get the environment linked to a source
 */
export declare function environmentsGetBySource(
  client: GramCore,
  request: GetSourceEnvironmentRequest,
  security?: GetSourceEnvironmentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Environment,
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
//# sourceMappingURL=environmentsGetBySource.d.ts.map
