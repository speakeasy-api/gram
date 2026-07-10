import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SetPluginAssignmentsResponseBody } from "../models/components/setpluginassignmentsresponsebody.js";
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
  SetPluginAssignmentsRequest,
  SetPluginAssignmentsSecurity,
} from "../models/operations/setpluginassignments.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setPluginAssignments plugins
 *
 * @remarks
 * Replace all assignments for a plugin with the given list of principal URNs.
 */
export declare function pluginsSetPluginAssignments(
  client: GramCore,
  request: SetPluginAssignmentsRequest,
  security?: SetPluginAssignmentsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SetPluginAssignmentsResponseBody,
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
//# sourceMappingURL=pluginsSetPluginAssignments.d.ts.map
