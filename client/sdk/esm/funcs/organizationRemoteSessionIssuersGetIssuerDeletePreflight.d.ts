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
 * getIssuerDeletePreflight organizationRemoteSessionIssuers
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersGetIssuerDeletePreflight(client: GramCore, request: operations.GetOrganizationRemoteSessionIssuerDeletePreflightRequest, security?: operations.GetOrganizationRemoteSessionIssuerDeletePreflightSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.OrganizationIssuerDeletePreflight, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersGetIssuerDeletePreflight.d.ts.map