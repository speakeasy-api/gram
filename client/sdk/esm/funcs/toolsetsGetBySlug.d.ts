import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
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
  GetToolsetRequest,
  GetToolsetSecurity,
} from "../models/operations/gettoolset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getToolset toolsets
 *
 * @remarks
 * Get detailed information about a toolset including full HTTP tool definitions
 */
export declare function toolsetsGetBySlug(
  client: GramCore,
  request: GetToolsetRequest,
  security?: GetToolsetSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Toolset,
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
//# sourceMappingURL=toolsetsGetBySlug.d.ts.map
