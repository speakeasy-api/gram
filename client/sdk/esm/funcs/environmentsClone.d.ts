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
  CloneEnvironmentRequest,
  CloneEnvironmentSecurity,
} from "../models/operations/cloneenvironment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * cloneEnvironment environments
 *
 * @remarks
 * Clone an environment into a new one. Either copies only the variable names with empty placeholder values, or copies the encrypted values verbatim. Encrypted secret values are never decrypted by the application during the clone operation.
 */
export declare function environmentsClone(
  client: GramCore,
  request: CloneEnvironmentRequest,
  security?: CloneEnvironmentSecurity | undefined,
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
//# sourceMappingURL=environmentsClone.d.ts.map
