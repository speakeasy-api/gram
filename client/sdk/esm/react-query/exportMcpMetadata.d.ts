import { MutationKey, UseMutationResult } from "@tanstack/react-query";
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
import { MutationHookOptions } from "./_types.js";
export type ExportMcpMetadataMutationVariables = {
  request: ExportMcpMetadataRequest;
  security?: ExportMcpMetadataSecurity | undefined;
  options?: RequestOptions;
};
export type ExportMcpMetadataMutationData = McpExport;
export type ExportMcpMetadataMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * exportMcpMetadata mcpMetadata
 *
 * @remarks
 * Export MCP server details as JSON for documentation and integration purposes.
 */
export declare function useExportMcpMetadataMutation(
  options?: MutationHookOptions<
    ExportMcpMetadataMutationData,
    ExportMcpMetadataMutationError,
    ExportMcpMetadataMutationVariables
  >,
): UseMutationResult<
  ExportMcpMetadataMutationData,
  ExportMcpMetadataMutationError,
  ExportMcpMetadataMutationVariables
>;
export declare function mutationKeyExportMcpMetadata(): MutationKey;
export declare function buildExportMcpMetadataMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ExportMcpMetadataMutationVariables,
  ) => Promise<ExportMcpMetadataMutationData>;
};
//# sourceMappingURL=exportMcpMetadata.d.ts.map
