import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskBlock } from "../models/components/riskblock.js";
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
  SubmitRiskBlockFeedbackRequest,
  SubmitRiskBlockFeedbackSecurity,
} from "../models/operations/submitriskblockfeedback.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * submitRiskBlockFeedback risk
 *
 * @remarks
 * Record thumbs-up/thumbs-down feedback for a tool call block from the block page.
 */
export declare function riskBlocksSubmitFeedback(
  client: GramCore,
  request: SubmitRiskBlockFeedbackRequest,
  security?: SubmitRiskBlockFeedbackSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskBlock,
    | ServiceError
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
//# sourceMappingURL=riskBlocksSubmitFeedback.d.ts.map
