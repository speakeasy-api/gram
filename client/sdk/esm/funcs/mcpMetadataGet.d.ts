import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetMcpMetadataResponseBody } from "../models/components/getmcpmetadataresponsebody.js";
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
  GetMcpMetadataRequest,
  GetMcpMetadataSecurity,
} from "../models/operations/getmcpmetadata.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getMcpMetadata mcpMetadata
 *
 * @remarks
 * Fetch the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
 */
export declare function mcpMetadataGet(
  client: GramCore,
  request?: GetMcpMetadataRequest | undefined,
  security?: GetMcpMetadataSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetMcpMetadataResponseBody,
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
//# sourceMappingURL=mcpMetadataGet.d.ts.map
