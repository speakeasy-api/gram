import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetTemplateRequest, GetTemplateSecurity } from "../models/operations/gettemplate.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildTemplateQuery, prefetchTemplate, queryKeyTemplate, TemplateQueryData } from "./template.core.js";
export { buildTemplateQuery, prefetchTemplate, queryKeyTemplate, type TemplateQueryData, };
export type TemplateQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getTemplate templates
 *
 * @remarks
 * Get prompt template by its ID or name.
 */
export declare function useTemplate(request?: GetTemplateRequest | undefined, security?: GetTemplateSecurity | undefined, options?: QueryHookOptions<TemplateQueryData, TemplateQueryError>): UseQueryResult<TemplateQueryData, TemplateQueryError>;
/**
 * getTemplate templates
 *
 * @remarks
 * Get prompt template by its ID or name.
 */
export declare function useTemplateSuspense(request?: GetTemplateRequest | undefined, security?: GetTemplateSecurity | undefined, options?: SuspenseQueryHookOptions<TemplateQueryData, TemplateQueryError>): UseSuspenseQueryResult<TemplateQueryData, TemplateQueryError>;
export declare function setTemplateData(client: QueryClient, queryKeyBase: [
    parameters: {
        id?: string | undefined;
        name?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: TemplateQueryData): TemplateQueryData | undefined;
export declare function invalidateTemplate(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id?: string | undefined;
        name?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllTemplate(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=template.d.ts.map