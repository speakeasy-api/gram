import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAuditLogFacetsResult } from "../models/components/listauditlogfacetsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAuditLogFacetsRequest, ListAuditLogFacetsSecurity } from "../models/operations/listauditlogfacets.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listFacets auditlogs
 *
 * @remarks
 * List available audit log facet values across organization and projects.
 */
export declare function auditlogsListFacets(client: GramCore, request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListAuditLogFacetsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=auditlogsListFacets.d.ts.map