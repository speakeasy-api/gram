import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DetachServerFromCollectionRequest,
  DetachServerFromCollectionSecurity,
} from "../models/operations/detachserverfromcollection.js";
import { MutationHookOptions } from "./_types.js";
export type CollectionsDetachServerMutationVariables = {
  request: DetachServerFromCollectionRequest;
  security?: DetachServerFromCollectionSecurity | undefined;
  options?: RequestOptions;
};
export type CollectionsDetachServerMutationData = void;
export type CollectionsDetachServerMutationError =
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
 * detachServer collections
 *
 * @remarks
 * Detach a server from a collection. Provide exactly one of toolset_id or mcp_server_id.
 */
export declare function useCollectionsDetachServerMutation(
  options?: MutationHookOptions<
    CollectionsDetachServerMutationData,
    CollectionsDetachServerMutationError,
    CollectionsDetachServerMutationVariables
  >,
): UseMutationResult<
  CollectionsDetachServerMutationData,
  CollectionsDetachServerMutationError,
  CollectionsDetachServerMutationVariables
>;
export declare function mutationKeyCollectionsDetachServer(): MutationKey;
export declare function buildCollectionsDetachServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CollectionsDetachServerMutationVariables,
  ) => Promise<CollectionsDetachServerMutationData>;
};
//# sourceMappingURL=collectionsDetachServer.d.ts.map
