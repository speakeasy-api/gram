import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskResultsForAgentRequest, ListRiskResultsForAgentSecurity } from "../models/operations/listriskresultsforagent.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskListResultsForAgentQuery, prefetchRiskListResultsForAgent, queryKeyRiskListResultsForAgent, RiskListResultsForAgentQueryData } from "./riskListResultsForAgent.core.js";
export { buildRiskListResultsForAgentQuery, prefetchRiskListResultsForAgent, queryKeyRiskListResultsForAgent, type RiskListResultsForAgentQueryData, };
export type RiskListResultsForAgentQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listRiskResultsForAgent risk
 *
 * @remarks
 * List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.
 */
export declare function useRiskListResultsForAgent(request?: ListRiskResultsForAgentRequest | undefined, security?: ListRiskResultsForAgentSecurity | undefined, options?: QueryHookOptions<RiskListResultsForAgentQueryData, RiskListResultsForAgentQueryError>): UseQueryResult<RiskListResultsForAgentQueryData, RiskListResultsForAgentQueryError>;
/**
 * listRiskResultsForAgent risk
 *
 * @remarks
 * List risk analysis results with the `match` field redacted to an opaque length+sha256-prefix fingerprint. Matches the payload and pagination semantics of listRiskResults. Designed for AI assistant / MCP consumption so secret content (gitleaks captures, presidio entities, prompt-injection payloads) never reaches the model context. For shadow_mcp findings the `match` value — a non-sensitive server URL or command identifier — is passed through verbatim.
 */
export declare function useRiskListResultsForAgentSuspense(request?: ListRiskResultsForAgentRequest | undefined, security?: ListRiskResultsForAgentSecurity | undefined, options?: SuspenseQueryHookOptions<RiskListResultsForAgentQueryData, RiskListResultsForAgentQueryError>): UseSuspenseQueryResult<RiskListResultsForAgentQueryData, RiskListResultsForAgentQueryError>;
export declare function setRiskListResultsForAgentData(client: QueryClient, queryKeyBase: [
    parameters: {
        policyId?: string | undefined;
        chatId?: string | undefined;
        category?: string | undefined;
        ruleId?: string | undefined;
        userId?: string | undefined;
        uniqueMatch?: boolean | undefined;
        from?: Date | undefined;
        to?: Date | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskListResultsForAgentQueryData): RiskListResultsForAgentQueryData | undefined;
export declare function invalidateRiskListResultsForAgent(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        policyId?: string | undefined;
        chatId?: string | undefined;
        category?: string | undefined;
        ruleId?: string | undefined;
        userId?: string | undefined;
        uniqueMatch?: boolean | undefined;
        from?: Date | undefined;
        to?: Date | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskListResultsForAgent(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskListResultsForAgent.d.ts.map