import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetLatestDeploymentResult } from "../models/components/getlatestdeploymentresult.js";
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
  GetLatestDeploymentRequest,
  GetLatestDeploymentSecurity,
} from "../models/operations/getlatestdeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getLatestDeployment deployments
 *
 * @remarks
 * Get the latest deployment for a project.
 */
export declare function deploymentsLatest(
  client: GramCore,
  request?: GetLatestDeploymentRequest | undefined,
  security?: GetLatestDeploymentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetLatestDeploymentResult,
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
//# sourceMappingURL=deploymentsLatest.d.ts.map
