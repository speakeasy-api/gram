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
  SetToolsetToolVariationsGroupRequest,
  SetToolsetToolVariationsGroupSecurity,
} from "../models/operations/settoolsettoolvariationsgroup.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setToolVariationsGroup toolsets
 *
 * @remarks
 * Assign a tool variations group to a toolset to enable MCP tool filtering (or pass null to disable). The group must already exist in the caller's project.
 */
export declare function toolsetsSetToolVariationsGroup(
  client: GramCore,
  request: SetToolsetToolVariationsGroupRequest,
  security?: SetToolsetToolVariationsGroupSecurity | undefined,
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
//# sourceMappingURL=toolsetsSetToolVariationsGroup.d.ts.map
