import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListDeploymentResult } from "../models/components/listdeploymentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListDeploymentsRequest, ListDeploymentsSecurity } from "../models/operations/listdeployments.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listDeployments deployments
 *
 * @remarks
 * List all deployments in descending order of creation.
 */
export declare function deploymentsList(client: GramCore, request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListDeploymentResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=deploymentsList.d.ts.map