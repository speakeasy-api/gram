import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationIssuerDeletePreflight } from "../models/components/organizationissuerdeletepreflight.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetOrganizationRemoteSessionIssuerDeletePreflightRequest,
  GetOrganizationRemoteSessionIssuerDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionissuerdeletepreflight.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getIssuerDeletePreflight organizationRemoteSessionIssuers
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersGetDeletePreflight(
  client: GramCore,
  request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionIssuerDeletePreflightSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    OrganizationIssuerDeletePreflight,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=organizationRemoteSessionIssuersGetDeletePreflight.d.ts.map
