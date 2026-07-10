import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ExprCompileResult } from "../models/components/exprcompileresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CompileExprRequest, CompileExprSecurity } from "../models/operations/compileexpr.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * compileExpr risk
 *
 * @remarks
 * Compile a single CEL expression (a detection predicate or a policy scope predicate) without evaluating it, so the editor can validate as the author types. Returns ok=true when it compiles, otherwise ok=false with the compiler error message. An empty expression is valid (ok=true).
 */
export declare function riskExprCompile(client: GramCore, request?: CompileExprRequest | undefined, security?: CompileExprSecurity | undefined, options?: RequestOptions): APIPromise<Result<ExprCompileResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskExprCompile.d.ts.map