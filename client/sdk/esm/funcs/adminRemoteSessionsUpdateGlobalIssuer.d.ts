import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateGlobalRemoteSessionIssuerRequest, UpdateGlobalRemoteSessionIssuerSecurity } from "../models/operations/updateglobalremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Update a global remote_session_issuer. Requires platform admin.
 */
export declare function adminRemoteSessionsUpdateGlobalIssuer(client: GramCore, request: UpdateGlobalRemoteSessionIssuerRequest, security?: UpdateGlobalRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionIssuer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=adminRemoteSessionsUpdateGlobalIssuer.d.ts.map