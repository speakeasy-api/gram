import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateDeploymentResult } from "../models/components/createdeploymentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateDeploymentRequest, CreateDeploymentSecurity } from "../models/operations/createdeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createDeployment deployments
 *
 * @remarks
 * Create a deployment to load tool definitions.
 */
export declare function deploymentsCreate(client: GramCore, request: CreateDeploymentRequest, security?: CreateDeploymentSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreateDeploymentResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=deploymentsCreate.d.ts.map