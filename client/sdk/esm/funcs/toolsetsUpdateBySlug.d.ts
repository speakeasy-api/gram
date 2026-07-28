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
  UpdateToolsetRequest,
  UpdateToolsetSecurity,
} from "../models/operations/updatetoolset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateToolset toolsets
 *
 * @remarks
 * Update a toolset's properties including name, description, and HTTP tools
 */
export declare function toolsetsUpdateBySlug(
  client: GramCore,
  request: UpdateToolsetRequest,
  security?: UpdateToolsetSecurity | undefined,
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
//# sourceMappingURL=toolsetsUpdateBySlug.d.ts.map
