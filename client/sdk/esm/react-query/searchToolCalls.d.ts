import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchToolCallsResult } from "../models/components/searchtoolcallsresult.js";
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
  SearchToolCallsRequest,
  SearchToolCallsSecurity,
} from "../models/operations/searchtoolcalls.js";
import { MutationHookOptions } from "./_types.js";
export type SearchToolCallsMutationVariables = {
  request: SearchToolCallsRequest;
  security?: SearchToolCallsSecurity | undefined;
  options?: RequestOptions;
};
export type SearchToolCallsMutationData = SearchToolCallsResult;
export type SearchToolCallsMutationError =
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
 * searchToolCalls telemetry
 *
 * @remarks
 * Search and list tool calls that match a search filter
 */
export declare function useSearchToolCallsMutation(
  options?: MutationHookOptions<
    SearchToolCallsMutationData,
    SearchToolCallsMutationError,
    SearchToolCallsMutationVariables
  >,
): UseMutationResult<
  SearchToolCallsMutationData,
  SearchToolCallsMutationError,
  SearchToolCallsMutationVariables
>;
export declare function mutationKeySearchToolCalls(): MutationKey;
export declare function buildSearchToolCallsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SearchToolCallsMutationVariables,
  ) => Promise<SearchToolCallsMutationData>;
};
//# sourceMappingURL=searchToolCalls.d.ts.map
