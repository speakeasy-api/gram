import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The result of compiling a single CEL expression for the editor.
 */
export type ExprCompileResult = {
    /**
     * Compiler error message when ok is false; empty otherwise.
     */
    error: string;
    /**
     * True when the expression compiled successfully.
     */
    ok: boolean;
};
/** @internal */
export declare const ExprCompileResult$inboundSchema: z.ZodMiniType<ExprCompileResult, unknown>;
export declare function exprCompileResultFromJSON(jsonString: string): SafeParseResult<ExprCompileResult, SDKValidationError>;
//# sourceMappingURL=exprcompileresult.d.ts.map