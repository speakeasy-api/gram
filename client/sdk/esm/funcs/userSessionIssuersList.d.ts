import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListUserSessionIssuersRequest, ListUserSessionIssuersResponse, ListUserSessionIssuersSecurity } from "../models/operations/listusersessionissuers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listUserSessionIssuers userSessionIssuers
 *
 * @remarks
 * List user_session_issuers in the caller's project.
 */
export declare function userSessionIssuersList(client: GramCore, request?: ListUserSessionIssuersRequest | undefined, security?: ListUserSessionIssuersSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListUserSessionIssuersResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=userSessionIssuersList.d.ts.map