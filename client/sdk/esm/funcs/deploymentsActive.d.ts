import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetActiveDeploymentResult } from "../models/components/getactivedeploymentresult.js";
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
  GetActiveDeploymentRequest,
  GetActiveDeploymentSecurity,
} from "../models/operations/getactivedeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getActiveDeployment deployments
 *
 * @remarks
 * Get the active deployment for a project.
 */
export declare function deploymentsActive(
  client: GramCore,
  request?: GetActiveDeploymentRequest | undefined,
  security?: GetActiveDeploymentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetActiveDeploymentResult,
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
//# sourceMappingURL=deploymentsActive.d.ts.map
