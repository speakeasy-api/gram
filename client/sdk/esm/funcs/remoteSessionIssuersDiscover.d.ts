import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuerDraft } from "../models/components/remotesessionissuerdraft.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DiscoverRemoteSessionIssuerRequest, DiscoverRemoteSessionIssuerSecurity } from "../models/operations/discoverremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * discoverRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createRemoteSessionIssuer. No persistence.
 */
export declare function remoteSessionIssuersDiscover(client: GramCore, request: DiscoverRemoteSessionIssuerRequest, security?: DiscoverRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionIssuerDraft, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionIssuersDiscover.d.ts.map