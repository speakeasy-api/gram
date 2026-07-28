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
  GetRiskBlockRequest,
  GetRiskBlockSecurity,
} from "../models/operations/getriskblock.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskBlock risk
 *
 * @remarks
 * Get a tool call block by its risk result ID for the durable block page.
 */
export declare function riskBlocksGet(
  client: GramCore,
  request: GetRiskBlockRequest,
  security?: GetRiskBlockSecurity | undefined,
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
//# sourceMappingURL=riskBlocksGet.d.ts.map
