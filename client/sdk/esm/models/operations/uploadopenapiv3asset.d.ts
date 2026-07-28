import * as z from "zod/v4-mini";
export type UploadOpenAPIv3AssetSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UploadOpenAPIv3AssetSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UploadOpenAPIv3AssetSecurity = {
  option1?: UploadOpenAPIv3AssetSecurityOption1 | undefined;
  option2?: UploadOpenAPIv3AssetSecurityOption2 | undefined;
};
export type UploadOpenAPIv3AssetRequest = {
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
export type UploadOpenAPIv3AssetSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UploadOpenAPIv3AssetSecurityOption1$outboundSchema: z.ZodMiniType<
  UploadOpenAPIv3AssetSecurityOption1$Outbound,
  UploadOpenAPIv3AssetSecurityOption1
>;
export declare function uploadOpenAPIv3AssetSecurityOption1ToJSON(
  uploadOpenAPIv3AssetSecurityOption1: UploadOpenAPIv3AssetSecurityOption1,
): string;
/** @internal */
export type UploadOpenAPIv3AssetSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UploadOpenAPIv3AssetSecurityOption2$outboundSchema: z.ZodMiniType<
  UploadOpenAPIv3AssetSecurityOption2$Outbound,
  UploadOpenAPIv3AssetSecurityOption2
>;
export declare function uploadOpenAPIv3AssetSecurityOption2ToJSON(
  uploadOpenAPIv3AssetSecurityOption2: UploadOpenAPIv3AssetSecurityOption2,
): string;
/** @internal */
export type UploadOpenAPIv3AssetSecurity$Outbound = {
  Option1?: UploadOpenAPIv3AssetSecurityOption1$Outbound | undefined;
  Option2?: UploadOpenAPIv3AssetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UploadOpenAPIv3AssetSecurity$outboundSchema: z.ZodMiniType<
  UploadOpenAPIv3AssetSecurity$Outbound,
  UploadOpenAPIv3AssetSecurity
>;
export declare function uploadOpenAPIv3AssetSecurityToJSON(
  uploadOpenAPIv3AssetSecurity: UploadOpenAPIv3AssetSecurity,
): string;
/** @internal */
export type UploadOpenAPIv3AssetRequest$Outbound = {
  "Content-Length": number;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Session"?: string | undefined;
  RequestBody: ReadableStream<Uint8Array> | Blob | ArrayBuffer | Uint8Array;
};
/** @internal */
export declare const UploadOpenAPIv3AssetRequest$outboundSchema: z.ZodMiniType<
  UploadOpenAPIv3AssetRequest$Outbound,
  UploadOpenAPIv3AssetRequest
>;
export declare function uploadOpenAPIv3AssetRequestToJSON(
  uploadOpenAPIv3AssetRequest: UploadOpenAPIv3AssetRequest,
): string;
//# sourceMappingURL=uploadopenapiv3asset.d.ts.map
