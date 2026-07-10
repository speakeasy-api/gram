import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteCollectionRequest, DeleteCollectionSecurity } from "../models/operations/deletecollection.js";
import { MutationHookOptions } from "./_types.js";
export type CollectionsDeleteMutationVariables = {
    request: DeleteCollectionRequest;
    security?: DeleteCollectionSecurity | undefined;
    options?: RequestOptions;
};
export type CollectionsDeleteMutationData = void;
export type CollectionsDeleteMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * delete collections
 *
 * @remarks
 * Delete an MCP collection
 */
export declare function useCollectionsDeleteMutation(options?: MutationHookOptions<CollectionsDeleteMutationData, CollectionsDeleteMutationError, CollectionsDeleteMutationVariables>): UseMutationResult<CollectionsDeleteMutationData, CollectionsDeleteMutationError, CollectionsDeleteMutationVariables>;
export declare function mutationKeyCollectionsDelete(): MutationKey;
export declare function buildCollectionsDeleteMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CollectionsDeleteMutationVariables) => Promise<CollectionsDeleteMutationData>;
};
//# sourceMappingURL=collectionsDelete.d.ts.map