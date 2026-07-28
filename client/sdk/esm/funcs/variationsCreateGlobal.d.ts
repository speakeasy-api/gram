import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ToolVariationGroupResult } from "../models/components/toolvariationgroupresult.js";
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
  CreateGlobalToolVariationGroupRequest,
  CreateGlobalToolVariationGroupSecurity,
} from "../models/operations/createglobaltoolvariationgroup.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createGlobal variations
 *
 * @remarks
 * Ensure the project-default (global) tool variation group exists, returning it. Idempotent: returns the existing group unchanged when present, otherwise creates it. Takes no parameters and only manages the single project-default group.
 */
export declare function variationsCreateGlobal(
  client: GramCore,
  request?: CreateGlobalToolVariationGroupRequest | undefined,
  security?: CreateGlobalToolVariationGroupSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ToolVariationGroupResult,
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
//# sourceMappingURL=variationsCreateGlobal.d.ts.map
