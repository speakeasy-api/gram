import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetDeploymentLogsResult } from "../models/components/getdeploymentlogsresult.js";
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
  GetDeploymentLogsRequest,
  GetDeploymentLogsSecurity,
} from "../models/operations/getdeploymentlogs.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getDeploymentLogs deployments
 *
 * @remarks
 * Get logs for a deployment.
 */
export declare function deploymentsLogs(
  client: GramCore,
  request: GetDeploymentLogsRequest,
  security?: GetDeploymentLogsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetDeploymentLogsResult,
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
//# sourceMappingURL=deploymentsLogs.d.ts.map
