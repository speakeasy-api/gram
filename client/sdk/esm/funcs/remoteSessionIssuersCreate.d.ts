import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteSessionIssuerRequest, CreateRemoteSessionIssuerSecurity } from "../models/operations/createremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Create a new remote_session_issuer.
 */
export declare function remoteSessionIssuersCreate(client: GramCore, request: CreateRemoteSessionIssuerRequest, security?: CreateRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionIssuer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionIssuersCreate.d.ts.map