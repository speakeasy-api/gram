import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
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
  UpdateCollectionRequest,
  UpdateCollectionSecurity,
} from "../models/operations/updatecollection.js";
import { MutationHookOptions } from "./_types.js";
export type CollectionsUpdateMutationVariables = {
  request: UpdateCollectionRequest;
  security?: UpdateCollectionSecurity | undefined;
  options?: RequestOptions;
};
export type CollectionsUpdateMutationData = MCPCollection;
export type CollectionsUpdateMutationError =
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
 * update collections
 *
 * @remarks
 * Update an MCP collection
 */
export declare function useCollectionsUpdateMutation(
  options?: MutationHookOptions<
    CollectionsUpdateMutationData,
    CollectionsUpdateMutationError,
    CollectionsUpdateMutationVariables
  >,
): UseMutationResult<
  CollectionsUpdateMutationData,
  CollectionsUpdateMutationError,
  CollectionsUpdateMutationVariables
>;
export declare function mutationKeyCollectionsUpdate(): MutationKey;
export declare function buildCollectionsUpdateMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CollectionsUpdateMutationVariables,
  ) => Promise<CollectionsUpdateMutationData>;
};
//# sourceMappingURL=collectionsUpdate.d.ts.map
