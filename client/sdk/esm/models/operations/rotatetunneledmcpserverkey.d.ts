import * as z from "zod/v4-mini";
import { RotateTunneledMcpServerKeyForm, RotateTunneledMcpServerKeyForm$Outbound } from "../components/rotatetunneledmcpserverkeyform.js";
export type RotateTunneledMcpServerKeySecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RotateTunneledMcpServerKeySecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RotateTunneledMcpServerKeySecurity = {
    option1?: RotateTunneledMcpServerKeySecurityOption1 | undefined;
    option2?: RotateTunneledMcpServerKeySecurityOption2 | undefined;
};
export type RotateTunneledMcpServerKeyRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    rotateTunneledMcpServerKeyForm: RotateTunneledMcpServerKeyForm;
};
/** @internal */
export type RotateTunneledMcpServerKeySecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RotateTunneledMcpServerKeySecurityOption1$outboundSchema: z.ZodMiniType<RotateTunneledMcpServerKeySecurityOption1$Outbound, RotateTunneledMcpServerKeySecurityOption1>;
export declare function rotateTunneledMcpServerKeySecurityOption1ToJSON(rotateTunneledMcpServerKeySecurityOption1: RotateTunneledMcpServerKeySecurityOption1): string;
/** @internal */
export type RotateTunneledMcpServerKeySecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RotateTunneledMcpServerKeySecurityOption2$outboundSchema: z.ZodMiniType<RotateTunneledMcpServerKeySecurityOption2$Outbound, RotateTunneledMcpServerKeySecurityOption2>;
export declare function rotateTunneledMcpServerKeySecurityOption2ToJSON(rotateTunneledMcpServerKeySecurityOption2: RotateTunneledMcpServerKeySecurityOption2): string;
/** @internal */
export type RotateTunneledMcpServerKeySecurity$Outbound = {
    Option1?: RotateTunneledMcpServerKeySecurityOption1$Outbound | undefined;
    Option2?: RotateTunneledMcpServerKeySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RotateTunneledMcpServerKeySecurity$outboundSchema: z.ZodMiniType<RotateTunneledMcpServerKeySecurity$Outbound, RotateTunneledMcpServerKeySecurity>;
export declare function rotateTunneledMcpServerKeySecurityToJSON(rotateTunneledMcpServerKeySecurity: RotateTunneledMcpServerKeySecurity): string;
/** @internal */
export type RotateTunneledMcpServerKeyRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RotateTunneledMcpServerKeyForm: RotateTunneledMcpServerKeyForm$Outbound;
};
/** @internal */
export declare const RotateTunneledMcpServerKeyRequest$outboundSchema: z.ZodMiniType<RotateTunneledMcpServerKeyRequest$Outbound, RotateTunneledMcpServerKeyRequest>;
export declare function rotateTunneledMcpServerKeyRequestToJSON(rotateTunneledMcpServerKeyRequest: RotateTunneledMcpServerKeyRequest): string;
//# sourceMappingURL=rotatetunneledmcpserverkey.d.ts.map