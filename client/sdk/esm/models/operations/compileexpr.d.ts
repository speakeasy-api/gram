import * as z from "zod/v4-mini";
export type CompileExprSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CompileExprSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CompileExprSecurity = {
    option1?: CompileExprSecurityOption1 | undefined;
    option2?: CompileExprSecurityOption2 | undefined;
};
export type CompileExprRequest = {
    /**
     * The CEL expression to compile. Empty is valid and compiles to ok=true.
     */
    expr?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type CompileExprSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CompileExprSecurityOption1$outboundSchema: z.ZodMiniType<CompileExprSecurityOption1$Outbound, CompileExprSecurityOption1>;
export declare function compileExprSecurityOption1ToJSON(compileExprSecurityOption1: CompileExprSecurityOption1): string;
/** @internal */
export type CompileExprSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CompileExprSecurityOption2$outboundSchema: z.ZodMiniType<CompileExprSecurityOption2$Outbound, CompileExprSecurityOption2>;
export declare function compileExprSecurityOption2ToJSON(compileExprSecurityOption2: CompileExprSecurityOption2): string;
/** @internal */
export type CompileExprSecurity$Outbound = {
    Option1?: CompileExprSecurityOption1$Outbound | undefined;
    Option2?: CompileExprSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CompileExprSecurity$outboundSchema: z.ZodMiniType<CompileExprSecurity$Outbound, CompileExprSecurity>;
export declare function compileExprSecurityToJSON(compileExprSecurity: CompileExprSecurity): string;
/** @internal */
export type CompileExprRequest$Outbound = {
    expr: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CompileExprRequest$outboundSchema: z.ZodMiniType<CompileExprRequest$Outbound, CompileExprRequest>;
export declare function compileExprRequestToJSON(compileExprRequest: CompileExprRequest): string;
//# sourceMappingURL=compileexpr.d.ts.map