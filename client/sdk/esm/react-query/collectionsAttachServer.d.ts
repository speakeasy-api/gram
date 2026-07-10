import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AttachServerToCollectionRequest, AttachServerToCollectionSecurity } from "../models/operations/attachservertocollection.js";
import { MutationHookOptions } from "./_types.js";
export type CollectionsAttachServerMutationVariables = {
    request: AttachServerToCollectionRequest;
    security?: AttachServerToCollectionSecurity | undefined;
    options?: RequestOptions;
};
export type CollectionsAttachServerMutationData = MCPCollection;
export type CollectionsAttachServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * attachServer collections
 *
 * @remarks
 * Attach a server to a collection. Provide exactly one of toolset_id or mcp_server_id.
 */
export declare function useCollectionsAttachServerMutation(options?: MutationHookOptions<CollectionsAttachServerMutationData, CollectionsAttachServerMutationError, CollectionsAttachServerMutationVariables>): UseMutationResult<CollectionsAttachServerMutationData, CollectionsAttachServerMutationError, CollectionsAttachServerMutationVariables>;
export declare function mutationKeyCollectionsAttachServer(): MutationKey;
export declare function buildCollectionsAttachServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CollectionsAttachServerMutationVariables) => Promise<CollectionsAttachServerMutationData>;
};
//# sourceMappingURL=collectionsAttachServer.d.ts.map