import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateEnvironmentRequest, CreateEnvironmentSecurity } from "../models/operations/createenvironment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createEnvironment environments
 *
 * @remarks
 * Create a new environment
 */
export declare function environmentsCreate(client: GramCore, request: CreateEnvironmentRequest, security?: CreateEnvironmentSecurity | undefined, options?: RequestOptions): APIPromise<Result<Environment, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=environmentsCreate.d.ts.map