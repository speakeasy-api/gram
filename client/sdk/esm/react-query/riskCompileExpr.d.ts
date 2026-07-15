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
  CompileExprRequest,
  CompileExprSecurity,
} from "../models/operations/compileexpr.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskCompileExprQuery,
  prefetchRiskCompileExpr,
  queryKeyRiskCompileExpr,
  RiskCompileExprQueryData,
} from "./riskCompileExpr.core.js";
export {
  buildRiskCompileExprQuery,
  prefetchRiskCompileExpr,
  queryKeyRiskCompileExpr,
  type RiskCompileExprQueryData,
};
export type RiskCompileExprQueryError =
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
 * compileExpr risk
 *
 * @remarks
 * Compile a single CEL expression (a detection predicate or a policy scope predicate) without evaluating it, so the editor can validate as the author types. Returns ok=true when it compiles, otherwise ok=false with the compiler error message. An empty expression is valid (ok=true).
 */
export declare function useRiskCompileExpr(
  request?: CompileExprRequest | undefined,
  security?: CompileExprSecurity | undefined,
  options?: QueryHookOptions<
    RiskCompileExprQueryData,
    RiskCompileExprQueryError
  >,
): UseQueryResult<RiskCompileExprQueryData, RiskCompileExprQueryError>;
/**
 * compileExpr risk
 *
 * @remarks
 * Compile a single CEL expression (a detection predicate or a policy scope predicate) without evaluating it, so the editor can validate as the author types. Returns ok=true when it compiles, otherwise ok=false with the compiler error message. An empty expression is valid (ok=true).
 */
export declare function useRiskCompileExprSuspense(
  request?: CompileExprRequest | undefined,
  security?: CompileExprSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskCompileExprQueryData,
    RiskCompileExprQueryError
  >,
): UseSuspenseQueryResult<RiskCompileExprQueryData, RiskCompileExprQueryError>;
export declare function setRiskCompileExprData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      expr?: string | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskCompileExprQueryData,
): RiskCompileExprQueryData | undefined;
export declare function invalidateRiskCompileExpr(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        expr?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskCompileExpr(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskCompileExpr.d.ts.map
