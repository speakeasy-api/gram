import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  CheckMCPSlugAvailabilityRequest,
  CheckMCPSlugAvailabilitySecurity,
} from "../models/operations/checkmcpslugavailability.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * checkMCPSlugAvailability toolsets
 *
 * @remarks
 * Check if a MCP slug is available
 */
export declare function toolsetsCheckMCPSlugAvailability(
  client: GramCore,
  request: CheckMCPSlugAvailabilityRequest,
  security?: CheckMCPSlugAvailabilitySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    boolean,
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
//# sourceMappingURL=toolsetsCheckMCPSlugAvailability.d.ts.map
