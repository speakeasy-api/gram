import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetProjectResult } from "../models/components/getprojectresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetProjectRequest, GetProjectSecurity } from "../models/operations/getproject.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getProject projects
 *
 * @remarks
 * Get project details by slug.
 */
export declare function projectsRead(client: GramCore, request: GetProjectRequest, security?: GetProjectSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetProjectResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsRead.d.ts.map