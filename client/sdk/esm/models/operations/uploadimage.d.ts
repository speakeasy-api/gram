import * as z from "zod/v4-mini";
export type UploadImageSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UploadImageSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UploadImageSecurity = {
    option1?: UploadImageSecurityOption1 | undefined;
    option2?: UploadImageSecurityOption2 | undefined;
};
export type UploadImageRequest = {
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
export type UploadImageSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UploadImageSecurityOption1$outboundSchema: z.ZodMiniType<UploadImageSecurityOption1$Outbound, UploadImageSecurityOption1>;
export declare function uploadImageSecurityOption1ToJSON(uploadImageSecurityOption1: UploadImageSecurityOption1): string;
/** @internal */
export type UploadImageSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UploadImageSecurityOption2$outboundSchema: z.ZodMiniType<UploadImageSecurityOption2$Outbound, UploadImageSecurityOption2>;
export declare function uploadImageSecurityOption2ToJSON(uploadImageSecurityOption2: UploadImageSecurityOption2): string;
/** @internal */
export type UploadImageSecurity$Outbound = {
    Option1?: UploadImageSecurityOption1$Outbound | undefined;
    Option2?: UploadImageSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UploadImageSecurity$outboundSchema: z.ZodMiniType<UploadImageSecurity$Outbound, UploadImageSecurity>;
export declare function uploadImageSecurityToJSON(uploadImageSecurity: UploadImageSecurity): string;
/** @internal */
export type UploadImageRequest$Outbound = {
    "Content-Length": number;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Session"?: string | undefined;
    RequestBody: ReadableStream<Uint8Array> | Blob | ArrayBuffer | Uint8Array;
};
/** @internal */
export declare const UploadImageRequest$outboundSchema: z.ZodMiniType<UploadImageRequest$Outbound, UploadImageRequest>;
export declare function uploadImageRequestToJSON(uploadImageRequest: UploadImageRequest): string;
//# sourceMappingURL=uploadimage.d.ts.map