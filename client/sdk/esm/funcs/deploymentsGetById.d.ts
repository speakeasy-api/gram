import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetDeploymentResult } from "../models/components/getdeploymentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetDeploymentRequest, GetDeploymentSecurity } from "../models/operations/getdeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getDeployment deployments
 *
 * @remarks
 * Get a deployment by its ID.
 */
export declare function deploymentsGetById(client: GramCore, request: GetDeploymentRequest, security?: GetDeploymentSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetDeploymentResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=deploymentsGetById.d.ts.map