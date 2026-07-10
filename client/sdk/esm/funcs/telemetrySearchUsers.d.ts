import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchUsersResult } from "../models/components/searchusersresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SearchUsersRequest, SearchUsersSecurity } from "../models/operations/searchusers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * searchUsers telemetry
 *
 * @remarks
 * Search and list user usage summaries grouped by user_id or external_user_id
 */
export declare function telemetrySearchUsers(client: GramCore, request: SearchUsersRequest, security?: SearchUsersSecurity | undefined, options?: RequestOptions): APIPromise<Result<SearchUsersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetrySearchUsers.d.ts.map