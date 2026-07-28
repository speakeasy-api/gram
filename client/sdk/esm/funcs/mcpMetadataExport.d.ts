import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpExport } from "../models/components/mcpexport.js";
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
  ExportMcpMetadataRequest,
  ExportMcpMetadataSecurity,
} from "../models/operations/exportmcpmetadata.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * exportMcpMetadata mcpMetadata
 *
 * @remarks
 * Export MCP server details as JSON for documentation and integration purposes.
 */
export declare function mcpMetadataExport(
  client: GramCore,
  request: ExportMcpMetadataRequest,
  security?: ExportMcpMetadataSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    McpExport,
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
//# sourceMappingURL=mcpMetadataExport.d.ts.map
