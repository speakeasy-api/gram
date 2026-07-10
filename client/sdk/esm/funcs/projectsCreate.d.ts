import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateProjectResult } from "../models/components/createprojectresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateProjectRequest, CreateProjectSecurity } from "../models/operations/createproject.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createProject projects
 *
 * @remarks
 * Create a new project.
 */
export declare function projectsCreate(client: GramCore, request: CreateProjectRequest, security?: CreateProjectSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreateProjectResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsCreate.d.ts.map