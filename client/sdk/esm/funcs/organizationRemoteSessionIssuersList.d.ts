import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListOrganizationRemoteSessionIssuersRequest, ListOrganizationRemoteSessionIssuersResponse, ListOrganizationRemoteSessionIssuersSecurity } from "../models/operations/listorganizationremotesessionissuers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersList(client: GramCore, request?: ListOrganizationRemoteSessionIssuersRequest | undefined, security?: ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListOrganizationRemoteSessionIssuersResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=organizationRemoteSessionIssuersList.d.ts.map