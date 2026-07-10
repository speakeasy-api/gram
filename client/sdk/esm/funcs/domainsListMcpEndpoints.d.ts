import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCustomDomainMcpEndpointsResult } from "../models/components/listcustomdomainmcpendpointsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListCustomDomainMcpEndpointsRequest, ListCustomDomainMcpEndpointsSecurity } from "../models/operations/listcustomdomainmcpendpoints.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listMcpEndpoints domains
 *
 * @remarks
 * List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.
 */
export declare function domainsListMcpEndpoints(client: GramCore, request?: ListCustomDomainMcpEndpointsRequest | undefined, security?: ListCustomDomainMcpEndpointsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListCustomDomainMcpEndpointsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=domainsListMcpEndpoints.d.ts.map