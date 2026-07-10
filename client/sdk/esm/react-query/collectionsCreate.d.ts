import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCollectionRequest, CreateCollectionSecurity } from "../models/operations/createcollection.js";
import { MutationHookOptions } from "./_types.js";
export type CollectionsCreateMutationVariables = {
    request: CreateCollectionRequest;
    security?: CreateCollectionSecurity | undefined;
    options?: RequestOptions;
};
export type CollectionsCreateMutationData = MCPCollection;
export type CollectionsCreateMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * create collections
 *
 * @remarks
 * Create an MCP collection within the organization
 */
export declare function useCollectionsCreateMutation(options?: MutationHookOptions<CollectionsCreateMutationData, CollectionsCreateMutationError, CollectionsCreateMutationVariables>): UseMutationResult<CollectionsCreateMutationData, CollectionsCreateMutationError, CollectionsCreateMutationVariables>;
export declare function mutationKeyCollectionsCreate(): MutationKey;
export declare function buildCollectionsCreateMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CollectionsCreateMutationVariables) => Promise<CollectionsCreateMutationData>;
};
//# sourceMappingURL=collectionsCreate.d.ts.map