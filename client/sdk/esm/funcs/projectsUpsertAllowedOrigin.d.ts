import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpsertAllowedOriginResult } from "../models/components/upsertallowedoriginresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpsertAllowedOriginRequest, UpsertAllowedOriginSecurity } from "../models/operations/upsertallowedorigin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * upsertAllowedOrigin projects
 *
 * @remarks
 * Upsert an allowed origin for a project.
 */
export declare function projectsUpsertAllowedOrigin(client: GramCore, request: UpsertAllowedOriginRequest, security?: UpsertAllowedOriginSecurity | undefined, options?: RequestOptions): APIPromise<Result<UpsertAllowedOriginResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=projectsUpsertAllowedOrigin.d.ts.map