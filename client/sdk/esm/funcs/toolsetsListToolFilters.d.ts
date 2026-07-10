import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolsetToolFiltersRequest, ListToolsetToolFiltersSecurity } from "../models/operations/listtoolsettoolfilters.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listToolFilters toolsets
 *
 * @remarks
 * List the tool filter scopes (tags) available on a toolset-backed MCP server and the tools under each, including tools excluded from all filters. Read-only; reflects the explicit tool variations group configured on the toolset, deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function toolsetsListToolFilters(client: GramCore, request: ListToolsetToolFiltersRequest, security?: ListToolsetToolFiltersSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListToolFiltersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=toolsetsListToolFilters.d.ts.map