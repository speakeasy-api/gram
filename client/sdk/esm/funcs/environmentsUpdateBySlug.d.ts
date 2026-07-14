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
  UpdateEnvironmentRequest,
  UpdateEnvironmentSecurity,
} from "../models/operations/updateenvironment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateEnvironment environments
 *
 * @remarks
 * Update an environment
 */
export declare function environmentsUpdateBySlug(
  client: GramCore,
  request: UpdateEnvironmentRequest,
  security?: UpdateEnvironmentSecurity | undefined,
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
//# sourceMappingURL=environmentsUpdateBySlug.d.ts.map
