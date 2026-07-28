import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ExternalMCPServer } from "../models/components/externalmcpserver.js";
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
  GetMCPServerDetailsRequest,
  GetMCPServerDetailsSecurity,
} from "../models/operations/getmcpserverdetails.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getServerDetails mcpRegistries
 *
 * @remarks
 * Get detailed information about an MCP server including remotes
 */
export declare function mcpRegistriesGetServerDetails(
  client: GramCore,
  request: GetMCPServerDetailsRequest,
  security?: GetMCPServerDetailsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ExternalMCPServer,
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
//# sourceMappingURL=mcpRegistriesGetServerDetails.d.ts.map
