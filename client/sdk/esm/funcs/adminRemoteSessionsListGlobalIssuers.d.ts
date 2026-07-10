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
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  ListGlobalRemoteSessionIssuersRequest,
  ListGlobalRemoteSessionIssuersResponse,
  ListGlobalRemoteSessionIssuersSecurity,
} from "../models/operations/listglobalremotesessionissuers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listGlobalIssuers adminRemoteSessions
 *
 * @remarks
 * List global remote_session_issuers. Requires platform admin.
 */
export declare function adminRemoteSessionsListGlobalIssuers(
  client: GramCore,
  request?: ListGlobalRemoteSessionIssuersRequest | undefined,
  security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      ListGlobalRemoteSessionIssuersResponse,
      | ServiceError
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
//# sourceMappingURL=adminRemoteSessionsListGlobalIssuers.d.ts.map
