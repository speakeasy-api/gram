import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListFilterOptionsResult } from "../models/components/listfilteroptionsresult.js";
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
  ListFilterOptionsRequest,
  ListFilterOptionsSecurity,
} from "../models/operations/listfilteroptions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listFilterOptions telemetry
 *
 * @remarks
 * List available filter options (API keys or users) for the observability overview
 */
export declare function telemetryListFilterOptions(
  client: GramCore,
  request: ListFilterOptionsRequest,
  security?: ListFilterOptionsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListFilterOptionsResult,
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
//# sourceMappingURL=telemetryListFilterOptions.d.ts.map
