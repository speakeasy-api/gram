import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetEmployeeDataFlowGraphResult } from "../models/components/getemployeedataflowgraphresult.js";
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
  GetEmployeeDataFlowGraphRequest,
  GetEmployeeDataFlowGraphSecurity,
} from "../models/operations/getemployeedataflowgraph.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getEmployeeDataFlowGraph telemetry
 *
 * @remarks
 * Get an employee's MCP data flow graph across origins, clients, servers, and tools
 */
export declare function telemetryGetEmployeeDataFlowGraph(
  client: GramCore,
  request: GetEmployeeDataFlowGraphRequest,
  security?: GetEmployeeDataFlowGraphSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetEmployeeDataFlowGraphResult,
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
//# sourceMappingURL=telemetryGetEmployeeDataFlowGraph.d.ts.map
