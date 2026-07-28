import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationClientDeletePreflight } from "../models/components/organizationclientdeletepreflight.js";
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
  GetOrganizationRemoteSessionClientDeletePreflightRequest,
  GetOrganizationRemoteSessionClientDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionclientdeletepreflight.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getClientDeletePreflight organizationRemoteSessionClients
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.
 */
export declare function organizationRemoteSessionClientsGetDeletePreflight(
  client: GramCore,
  request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionClientDeletePreflightSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    OrganizationClientDeletePreflight,
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
//# sourceMappingURL=organizationRemoteSessionClientsGetDeletePreflight.d.ts.map
