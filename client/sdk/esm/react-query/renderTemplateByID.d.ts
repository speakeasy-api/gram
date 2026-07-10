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
  RenderTemplateByIDRequest,
  RenderTemplateByIDSecurity,
} from "../models/operations/rendertemplatebyid.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRenderTemplateByIDQuery,
  prefetchRenderTemplateByID,
  queryKeyRenderTemplateByID,
  RenderTemplateByIDQueryData,
} from "./renderTemplateByID.core.js";
export {
  buildRenderTemplateByIDQuery,
  prefetchRenderTemplateByID,
  queryKeyRenderTemplateByID,
  type RenderTemplateByIDQueryData,
};
export type RenderTemplateByIDQueryError =
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
 * renderTemplateByID templates
 *
 * @remarks
 * Render a prompt template by ID with provided input data.
 */
export declare function useRenderTemplateByID(
  request: RenderTemplateByIDRequest,
  security?: RenderTemplateByIDSecurity | undefined,
  options?: QueryHookOptions<
    RenderTemplateByIDQueryData,
    RenderTemplateByIDQueryError
  >,
): UseQueryResult<RenderTemplateByIDQueryData, RenderTemplateByIDQueryError>;
/**
 * renderTemplateByID templates
 *
 * @remarks
 * Render a prompt template by ID with provided input data.
 */
export declare function useRenderTemplateByIDSuspense(
  request: RenderTemplateByIDRequest,
  security?: RenderTemplateByIDSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RenderTemplateByIDQueryData,
    RenderTemplateByIDQueryError
  >,
): UseSuspenseQueryResult<
  RenderTemplateByIDQueryData,
  RenderTemplateByIDQueryError
>;
export declare function setRenderTemplateByIDData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RenderTemplateByIDQueryData,
): RenderTemplateByIDQueryData | undefined;
export declare function invalidateRenderTemplateByID(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRenderTemplateByID(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=renderTemplateByID.d.ts.map
