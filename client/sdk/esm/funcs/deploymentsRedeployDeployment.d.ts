import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RedeployResult } from "../models/components/redeployresult.js";
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
  RedeployDeploymentRequest,
  RedeployDeploymentSecurity,
} from "../models/operations/redeploydeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * redeploy deployments
 *
 * @remarks
 * Redeploys an existing deployment.
 */
export declare function deploymentsRedeployDeployment(
  client: GramCore,
  request: RedeployDeploymentRequest,
  security?: RedeployDeploymentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RedeployResult,
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
//# sourceMappingURL=deploymentsRedeployDeployment.d.ts.map
