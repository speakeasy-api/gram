import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetManagedAssistantRequest, GetManagedAssistantSecurity } from "../models/operations/getmanagedassistant.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AssistantsGetManagedQueryData, buildAssistantsGetManagedQuery, prefetchAssistantsGetManaged, queryKeyAssistantsGetManaged } from "./assistantsGetManaged.core.js";
export { type AssistantsGetManagedQueryData, buildAssistantsGetManagedQuery, prefetchAssistantsGetManaged, queryKeyAssistantsGetManaged, };
export type AssistantsGetManagedQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getManagedAssistant assistants
 *
 * @remarks
 * Get the project's built-in Project Assistant if it exists. Returns 404 when no managed assistant has been provisioned yet — call ensureManagedAssistant to create one.
 */
export declare function useAssistantsGetManaged(request?: GetManagedAssistantRequest | undefined, security?: GetManagedAssistantSecurity | undefined, options?: QueryHookOptions<AssistantsGetManagedQueryData, AssistantsGetManagedQueryError>): UseQueryResult<AssistantsGetManagedQueryData, AssistantsGetManagedQueryError>;
/**
 * getManagedAssistant assistants
 *
 * @remarks
 * Get the project's built-in Project Assistant if it exists. Returns 404 when no managed assistant has been provisioned yet — call ensureManagedAssistant to create one.
 */
export declare function useAssistantsGetManagedSuspense(request?: GetManagedAssistantRequest | undefined, security?: GetManagedAssistantSecurity | undefined, options?: SuspenseQueryHookOptions<AssistantsGetManagedQueryData, AssistantsGetManagedQueryError>): UseSuspenseQueryResult<AssistantsGetManagedQueryData, AssistantsGetManagedQueryError>;
export declare function setAssistantsGetManagedData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: AssistantsGetManagedQueryData): AssistantsGetManagedQueryData | undefined;
export declare function invalidateAssistantsGetManaged(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAssistantsGetManaged(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=assistantsGetManaged.d.ts.map