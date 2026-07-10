import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskCapabilitiesQuery, prefetchRiskCapabilities, queryKeyRiskCapabilities, RiskCapabilitiesQueryData } from "./riskCapabilities.core.js";
export { buildRiskCapabilitiesQuery, prefetchRiskCapabilities, queryKeyRiskCapabilities, type RiskCapabilitiesQueryData, };
export type RiskCapabilitiesQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskCapabilities risk
 *
 * @remarks
 * Get server-side risk analysis capabilities for the current project.
 */
export declare function useRiskCapabilities(request?: operations.GetRiskCapabilitiesRequest | undefined, security?: operations.GetRiskCapabilitiesSecurity | undefined, options?: QueryHookOptions<RiskCapabilitiesQueryData, RiskCapabilitiesQueryError>): UseQueryResult<RiskCapabilitiesQueryData, RiskCapabilitiesQueryError>;
/**
 * getRiskCapabilities risk
 *
 * @remarks
 * Get server-side risk analysis capabilities for the current project.
 */
export declare function useRiskCapabilitiesSuspense(request?: operations.GetRiskCapabilitiesRequest | undefined, security?: operations.GetRiskCapabilitiesSecurity | undefined, options?: SuspenseQueryHookOptions<RiskCapabilitiesQueryData, RiskCapabilitiesQueryError>): UseSuspenseQueryResult<RiskCapabilitiesQueryData, RiskCapabilitiesQueryError>;
export declare function setRiskCapabilitiesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskCapabilitiesQueryData): RiskCapabilitiesQueryData | undefined;
export declare function invalidateRiskCapabilities(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskCapabilities(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskCapabilities.d.ts.map