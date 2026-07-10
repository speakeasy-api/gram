import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RedeemRequestBody } from "../models/components/redeemrequestbody.js";
import { RedeemResponseBody } from "../models/components/redeemresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * redeem cliAuth
 *
 * @remarks
 * Exchange a one-time code plus its PKCE code_verifier for a freshly minted per-user [agent,hooks] API key. No session or API-key auth: proving knowledge of the code_verifier that matches the stored challenge IS the credential. The code is single-use — consumed atomically on lookup — so any missing/expired/already-consumed code or PKCE mismatch returns 401. The raw key is returned exactly once and never again.
 */
export declare function cliAuthRedeem(client: GramCore, request: RedeemRequestBody, options?: RequestOptions): APIPromise<Result<RedeemResponseBody, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=cliAuthRedeem.d.ts.map