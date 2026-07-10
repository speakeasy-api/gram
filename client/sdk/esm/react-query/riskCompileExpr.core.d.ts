import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ExprCompileResult } from "../models/components/exprcompileresult.js";
import { CompileExprRequest, CompileExprSecurity } from "../models/operations/compileexpr.js";
export type RiskCompileExprQueryData = ExprCompileResult;
export declare function prefetchRiskCompileExpr(queryClient: QueryClient, client$: GramCore, request?: CompileExprRequest | undefined, security?: CompileExprSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskCompileExprQuery(client$: GramCore, request?: CompileExprRequest | undefined, security?: CompileExprSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskCompileExprQueryData>;
};
export declare function queryKeyRiskCompileExpr(parameters: {
    expr?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskCompileExpr.core.d.ts.map