import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetAIIntegrationConfigRequest, GetAIIntegrationConfigSecurity } from "../models/operations/getaiintegrationconfig.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AiIntegrationConfigQueryData, buildAiIntegrationConfigQuery, prefetchAiIntegrationConfig, queryKeyAiIntegrationConfig } from "./aiIntegrationConfig.core.js";
export { type AiIntegrationConfigQueryData, buildAiIntegrationConfigQuery, prefetchAiIntegrationConfig, queryKeyAiIntegrationConfig, };
export type AiIntegrationConfigQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getConfig aiIntegrations
 *
 * @remarks
 * Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.
 */
export declare function useAiIntegrationConfig(request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: QueryHookOptions<AiIntegrationConfigQueryData, AiIntegrationConfigQueryError>): UseQueryResult<AiIntegrationConfigQueryData, AiIntegrationConfigQueryError>;
/**
 * getConfig aiIntegrations
 *
 * @remarks
 * Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.
 */
export declare function useAiIntegrationConfigSuspense(request: GetAIIntegrationConfigRequest, security?: GetAIIntegrationConfigSecurity | undefined, options?: SuspenseQueryHookOptions<AiIntegrationConfigQueryData, AiIntegrationConfigQueryError>): UseSuspenseQueryResult<AiIntegrationConfigQueryData, AiIntegrationConfigQueryError>;
export declare function setAiIntegrationConfigData(client: QueryClient, queryKeyBase: [
    parameters: {
        provider: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: AiIntegrationConfigQueryData): AiIntegrationConfigQueryData | undefined;
export declare function invalidateAiIntegrationConfig(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        provider: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAiIntegrationConfig(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=aiIntegrationConfig.d.ts.map