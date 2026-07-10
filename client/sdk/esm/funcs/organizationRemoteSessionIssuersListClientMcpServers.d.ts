import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listClientMcpServers organizationRemoteSessionIssuers
 *
 * @remarks
 * List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersListClientMcpServers(client: GramCore, request: operations.ListOrganizationRemoteSessionClientMcpServersRequest, security?: operations.ListOrganizationRemoteSessionClientMcpServersSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.ListOrganizationMcpServersResult, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersListClientMcpServers.d.ts.map