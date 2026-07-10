import * as z from "zod/v4-mini";
import { RenderTemplateByIDRequestBody, RenderTemplateByIDRequestBody$Outbound } from "../components/rendertemplatebyidrequestbody.js";
export type RenderTemplateByIDSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RenderTemplateByIDSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RenderTemplateByIDSecurityOption3 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RenderTemplateByIDSecurity = {
    option1?: RenderTemplateByIDSecurityOption1 | undefined;
    option2?: RenderTemplateByIDSecurityOption2 | undefined;
    option3?: RenderTemplateByIDSecurityOption3 | undefined;
};
export type RenderTemplateByIDRequest = {
    /**
     * The ID of the prompt template to render
     */
    id: string;
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
    renderTemplateByIDRequestBody: RenderTemplateByIDRequestBody;
};
/** @internal */
export type RenderTemplateByIDSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RenderTemplateByIDSecurityOption1$outboundSchema: z.ZodMiniType<RenderTemplateByIDSecurityOption1$Outbound, RenderTemplateByIDSecurityOption1>;
export declare function renderTemplateByIDSecurityOption1ToJSON(renderTemplateByIDSecurityOption1: RenderTemplateByIDSecurityOption1): string;
/** @internal */
export type RenderTemplateByIDSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RenderTemplateByIDSecurityOption2$outboundSchema: z.ZodMiniType<RenderTemplateByIDSecurityOption2$Outbound, RenderTemplateByIDSecurityOption2>;
export declare function renderTemplateByIDSecurityOption2ToJSON(renderTemplateByIDSecurityOption2: RenderTemplateByIDSecurityOption2): string;
/** @internal */
export type RenderTemplateByIDSecurityOption3$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RenderTemplateByIDSecurityOption3$outboundSchema: z.ZodMiniType<RenderTemplateByIDSecurityOption3$Outbound, RenderTemplateByIDSecurityOption3>;
export declare function renderTemplateByIDSecurityOption3ToJSON(renderTemplateByIDSecurityOption3: RenderTemplateByIDSecurityOption3): string;
/** @internal */
export type RenderTemplateByIDSecurity$Outbound = {
    Option1?: RenderTemplateByIDSecurityOption1$Outbound | undefined;
    Option2?: RenderTemplateByIDSecurityOption2$Outbound | undefined;
    Option3?: RenderTemplateByIDSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const RenderTemplateByIDSecurity$outboundSchema: z.ZodMiniType<RenderTemplateByIDSecurity$Outbound, RenderTemplateByIDSecurity>;
export declare function renderTemplateByIDSecurityToJSON(renderTemplateByIDSecurity: RenderTemplateByIDSecurity): string;
/** @internal */
export type RenderTemplateByIDRequest$Outbound = {
    id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RenderTemplateByIDRequestBody: RenderTemplateByIDRequestBody$Outbound;
};
/** @internal */
export declare const RenderTemplateByIDRequest$outboundSchema: z.ZodMiniType<RenderTemplateByIDRequest$Outbound, RenderTemplateByIDRequest>;
export declare function renderTemplateByIDRequestToJSON(renderTemplateByIDRequest: RenderTemplateByIDRequest): string;
//# sourceMappingURL=rendertemplatebyid.d.ts.map