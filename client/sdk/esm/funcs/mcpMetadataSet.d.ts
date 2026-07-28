import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpMetadata } from "../models/components/mcpmetadata.js";
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
  SetMcpMetadataRequest,
  SetMcpMetadataSecurity,
} from "../models/operations/setmcpmetadata.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setMcpMetadata mcpMetadata
 *
 * @remarks
 * Create or update the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
 */
export declare function mcpMetadataSet(
  client: GramCore,
  request: SetMcpMetadataRequest,
  security?: SetMcpMetadataSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    McpMetadata,
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
//# sourceMappingURL=mcpMetadataSet.d.ts.map
