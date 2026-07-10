import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { TriggerRiskAnalysisRequest, TriggerRiskAnalysisSecurity } from "../models/operations/triggerriskanalysis.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * triggerRiskAnalysis risk
 *
 * @remarks
 * Manually trigger risk analysis for a policy, starting or signaling the drain workflow. Defaults to the most recent 100 unanalyzed messages; pass `limit=0` to backfill every unanalyzed message.
 */
export declare function riskPoliciesTrigger(client: GramCore, request: TriggerRiskAnalysisRequest, security?: TriggerRiskAnalysisSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPoliciesTrigger.d.ts.map