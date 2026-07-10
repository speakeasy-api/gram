import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTriggerDefinitionsRequest, ListTriggerDefinitionsSecurity } from "../models/operations/listtriggerdefinitions.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildTriggerDefinitionsQuery, prefetchTriggerDefinitions, queryKeyTriggerDefinitions, TriggerDefinitionsQueryData } from "./triggerDefinitions.core.js";
export { buildTriggerDefinitionsQuery, prefetchTriggerDefinitions, queryKeyTriggerDefinitions, type TriggerDefinitionsQueryData, };
export type TriggerDefinitionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listTriggerDefinitions triggers
 *
 * @remarks
 * List static trigger definitions available to a project.
 */
export declare function useTriggerDefinitions(request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: QueryHookOptions<TriggerDefinitionsQueryData, TriggerDefinitionsQueryError>): UseQueryResult<TriggerDefinitionsQueryData, TriggerDefinitionsQueryError>;
/**
 * listTriggerDefinitions triggers
 *
 * @remarks
 * List static trigger definitions available to a project.
 */
export declare function useTriggerDefinitionsSuspense(request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: SuspenseQueryHookOptions<TriggerDefinitionsQueryData, TriggerDefinitionsQueryError>): UseSuspenseQueryResult<TriggerDefinitionsQueryData, TriggerDefinitionsQueryError>;
export declare function setTriggerDefinitionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: TriggerDefinitionsQueryData): TriggerDefinitionsQueryData | undefined;
export declare function invalidateTriggerDefinitions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllTriggerDefinitions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=triggerDefinitions.d.ts.map