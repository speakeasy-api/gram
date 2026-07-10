import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetToolUsageFilterOptionsResult } from "../models/components/gettoolusagefilteroptionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetToolUsageFilterOptionsRequest, GetToolUsageFilterOptionsSecurity } from "../models/operations/gettoolusagefilteroptions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getToolUsageFilterOptions telemetry
 *
 * @remarks
 * Get filter options for target-aware MCP and tool usage metrics
 */
export declare function telemetryGetToolUsageFilterOptions(client: GramCore, request: GetToolUsageFilterOptionsRequest, security?: GetToolUsageFilterOptionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetToolUsageFilterOptionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryGetToolUsageFilterOptions.d.ts.map