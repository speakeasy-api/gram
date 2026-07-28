import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
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
  ListTemplatesRequest,
  ListTemplatesSecurity,
} from "../models/operations/listtemplates.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildTemplatesQuery,
  prefetchTemplates,
  queryKeyTemplates,
  TemplatesQueryData,
} from "./templates.core.js";
export {
  buildTemplatesQuery,
  prefetchTemplates,
  queryKeyTemplates,
  type TemplatesQueryData,
};
export type TemplatesQueryError =
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
 * listTemplates templates
 *
 * @remarks
 * List available prompt template.
 */
export declare function useTemplates(
  request?: ListTemplatesRequest | undefined,
  security?: ListTemplatesSecurity | undefined,
  options?: QueryHookOptions<TemplatesQueryData, TemplatesQueryError>,
): UseQueryResult<TemplatesQueryData, TemplatesQueryError>;
/**
 * listTemplates templates
 *
 * @remarks
 * List available prompt template.
 */
export declare function useTemplatesSuspense(
  request?: ListTemplatesRequest | undefined,
  security?: ListTemplatesSecurity | undefined,
  options?: SuspenseQueryHookOptions<TemplatesQueryData, TemplatesQueryError>,
): UseSuspenseQueryResult<TemplatesQueryData, TemplatesQueryError>;
export declare function setTemplatesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: TemplatesQueryData,
): TemplatesQueryData | undefined;
export declare function invalidateTemplates(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllTemplates(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=templates.d.ts.map
