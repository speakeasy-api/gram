import * as z from "zod/v4-mini";
export type UploadFunctionsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UploadFunctionsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UploadFunctionsSecurity = {
    option1?: UploadFunctionsSecurityOption1 | undefined;
    option2?: UploadFunctionsSecurityOption2 | undefined;
};
export type UploadFunctionsRequest = {
    contentLength: number;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    requestBody: ReadableStream<Uint8Array> | Blob | ArrayBuffer | Uint8Array;
};
/** @internal */
export type UploadFunctionsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UploadFunctionsSecurityOption1$outboundSchema: z.ZodMiniType<UploadFunctionsSecurityOption1$Outbound, UploadFunctionsSecurityOption1>;
export declare function uploadFunctionsSecurityOption1ToJSON(uploadFunctionsSecurityOption1: UploadFunctionsSecurityOption1): string;
/** @internal */
export type UploadFunctionsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UploadFunctionsSecurityOption2$outboundSchema: z.ZodMiniType<UploadFunctionsSecurityOption2$Outbound, UploadFunctionsSecurityOption2>;
export declare function uploadFunctionsSecurityOption2ToJSON(uploadFunctionsSecurityOption2: UploadFunctionsSecurityOption2): string;
/** @internal */
export type UploadFunctionsSecurity$Outbound = {
    Option1?: UploadFunctionsSecurityOption1$Outbound | undefined;
    Option2?: UploadFunctionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UploadFunctionsSecurity$outboundSchema: z.ZodMiniType<UploadFunctionsSecurity$Outbound, UploadFunctionsSecurity>;
export declare function uploadFunctionsSecurityToJSON(uploadFunctionsSecurity: UploadFunctionsSecurity): string;
/** @internal */
export type UploadFunctionsRequest$Outbound = {
    "Content-Length": number;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Session"?: string | undefined;
    RequestBody: ReadableStream<Uint8Array> | Blob | ArrayBuffer | Uint8Array;
};
/** @internal */
export declare const UploadFunctionsRequest$outboundSchema: z.ZodMiniType<UploadFunctionsRequest$Outbound, UploadFunctionsRequest>;
export declare function uploadFunctionsRequestToJSON(uploadFunctionsRequest: UploadFunctionsRequest): string;
//# sourceMappingURL=uploadfunctions.d.ts.map