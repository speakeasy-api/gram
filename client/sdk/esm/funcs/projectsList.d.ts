import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListProjectsResult } from "../models/components/listprojectsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListProjectsRequest, ListProjectsSecurity } from "../models/operations/listprojects.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listProjects projects
 *
 * @remarks
 * List all projects for an organization.
 */
export declare function projectsList(client: GramCore, request: ListProjectsRequest, security?: ListProjectsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListProjectsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsList.d.ts.map