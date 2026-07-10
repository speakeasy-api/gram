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
  ListRemoteSessionIssuersRequest,
  ListRemoteSessionIssuersResponse,
  ListRemoteSessionIssuersSecurity,
} from "../models/operations/listremotesessionissuers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listRemoteSessionIssuers remoteSessionIssuers
 *
 * @remarks
 * List remote_session_issuers in the caller's project.
 */
export declare function remoteSessionIssuersList(
  client: GramCore,
  request?: ListRemoteSessionIssuersRequest | undefined,
  security?: ListRemoteSessionIssuersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      ListRemoteSessionIssuersResponse,
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
//# sourceMappingURL=remoteSessionIssuersList.d.ts.map
