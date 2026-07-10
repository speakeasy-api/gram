import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listClientSessions organizationRemoteSessionIssuers
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersListClientSessions(
  client: GramCore,
  request: operations.ListOrganizationRemoteSessionClientSessionsRequest,
  security?:
    | operations.ListOrganizationRemoteSessionClientSessionsSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      operations.ListOrganizationRemoteSessionClientSessionsResponse,
      | errors.ServiceError
      | GramError
      | ResponseValidationError
      | ConnectionError
      | RequestAbortedError
      | RequestTimeoutError
      | InvalidRequestError
      | UnexpectedClientError
      | SDKValidationError
    >,
    {
      cursor: string;
    }
  >
>;
//# sourceMappingURL=organizationRemoteSessionIssuersListClientSessions.d.ts.map
