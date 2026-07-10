import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
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
/**
 * registerRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Perform an RFC 7591 Dynamic Client Registration call against an existing issuer's registration_endpoint and persist the issued credentials as a new remote_session_client. The issuer must already have a registration_endpoint configured.
 */
export declare function remoteSessionIssuersRegister(
  client: GramCore,
  request: operations.RegisterRemoteSessionIssuerRequest,
  security?: operations.RegisterRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.RemoteSessionClient,
    | errors.ServiceError
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
//# sourceMappingURL=remoteSessionIssuersRegister.d.ts.map
