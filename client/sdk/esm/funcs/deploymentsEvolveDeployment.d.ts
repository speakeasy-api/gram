import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { EvolveResult } from "../models/components/evolveresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { EvolveDeploymentRequest, EvolveDeploymentSecurity } from "../models/operations/evolvedeployment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * evolve deployments
 *
 * @remarks
 * Create a new deployment with additional or updated tool sources.
 */
export declare function deploymentsEvolveDeployment(client: GramCore, request: EvolveDeploymentRequest, security?: EvolveDeploymentSecurity | undefined, options?: RequestOptions): APIPromise<Result<EvolveResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=deploymentsEvolveDeployment.d.ts.map