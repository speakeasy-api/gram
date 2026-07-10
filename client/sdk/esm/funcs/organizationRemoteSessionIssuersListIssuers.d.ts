import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersListIssuers(client: GramCore, request?: operations.ListOrganizationRemoteSessionIssuersRequest | undefined, security?: operations.ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<operations.ListOrganizationRemoteSessionIssuersResponse, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=organizationRemoteSessionIssuersListIssuers.d.ts.map