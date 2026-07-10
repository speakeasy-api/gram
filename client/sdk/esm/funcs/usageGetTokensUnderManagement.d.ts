import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TokensUnderManagement } from "../models/components/tokensundermanagement.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetTokensUnderManagementRequest, GetTokensUnderManagementSecurity } from "../models/operations/gettokensundermanagement.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getTokensUnderManagement usage
 *
 * @remarks
 * Get tokens under management for the active billing cycle alongside the contracted terms
 */
export declare function usageGetTokensUnderManagement(client: GramCore, request?: GetTokensUnderManagementRequest | undefined, security?: GetTokensUnderManagementSecurity | undefined, options?: RequestOptions): APIPromise<Result<TokensUnderManagement, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=usageGetTokensUnderManagement.d.ts.map