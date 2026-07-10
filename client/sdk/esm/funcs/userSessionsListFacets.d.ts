import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUserSessionFacetsResult } from "../models/components/listusersessionfacetsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListUserSessionFacetsRequest, ListUserSessionFacetsSecurity } from "../models/operations/listusersessionfacets.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listFacets userSessions
 *
 * @remarks
 * List available user session facet values (clients, users, servers) in the caller's project.
 */
export declare function userSessionsListFacets(client: GramCore, request?: ListUserSessionFacetsRequest | undefined, security?: ListUserSessionFacetsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListUserSessionFacetsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=userSessionsListFacets.d.ts.map