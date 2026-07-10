import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskUnmaskResultResult } from "../models/components/riskunmaskresultresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UnmaskRiskResultRequest, UnmaskRiskResultSecurity } from "../models/operations/unmaskriskresult.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * unmaskRiskResult risk
 *
 * @remarks
 * Return the plaintext match for a single risk result, on demand. Gated on the chat:read scope for the result's chat (not org:admin) — reveal is a discrete, audited access event distinct from listing redacted results.
 */
export declare function riskResultsUnmask(client: GramCore, request: UnmaskRiskResultRequest, security?: UnmaskRiskResultSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskUnmaskResultResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskResultsUnmask.d.ts.map