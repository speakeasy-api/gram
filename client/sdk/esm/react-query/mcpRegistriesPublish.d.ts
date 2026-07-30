import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type McpRegistriesPublishMutationVariables = {
  request: operations.PublishMCPRegistryRequest;
  security?: operations.PublishMCPRegistrySecurity | undefined;
  options?: RequestOptions;
};
export type McpRegistriesPublishMutationData = components.MCPRegistry;
export type McpRegistriesPublishMutationError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * publish mcpRegistries
 *
 * @remarks
 * Publish toolsets as an internal MCP registry catalog
 */
export declare function useMcpRegistriesPublishMutation(
  options?: MutationHookOptions<
    McpRegistriesPublishMutationData,
    McpRegistriesPublishMutationError,
    McpRegistriesPublishMutationVariables
  >,
): UseMutationResult<
  McpRegistriesPublishMutationData,
  McpRegistriesPublishMutationError,
  McpRegistriesPublishMutationVariables
>;
export declare function mutationKeyMcpRegistriesPublish(): MutationKey;
export declare function buildMcpRegistriesPublishMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: McpRegistriesPublishMutationVariables,
  ) => Promise<McpRegistriesPublishMutationData>;
};
//# sourceMappingURL=mcpRegistriesPublish.d.ts.map
