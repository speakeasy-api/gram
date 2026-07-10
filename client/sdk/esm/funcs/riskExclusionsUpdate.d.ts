import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskExclusion } from "../models/components/riskexclusion.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateRiskExclusionRequest, UpdateRiskExclusionSecurity } from "../models/operations/updateriskexclusion.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateRiskExclusion risk
 *
 * @remarks
 * Update a risk exclusion.
 */
export declare function riskExclusionsUpdate(client: GramCore, request: UpdateRiskExclusionRequest, security?: UpdateRiskExclusionSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskExclusion, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskExclusionsUpdate.d.ts.map