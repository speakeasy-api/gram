import * as z from "zod/v4-mini";
export type RenderTemplateByIDRequestBody = {
    /**
     * The input data to render the template with
     */
    arguments: {
        [k: string]: any;
    };
};
/** @internal */
export type RenderTemplateByIDRequestBody$Outbound = {
    arguments: {
        [k: string]: any;
    };
};
/** @internal */
export declare const RenderTemplateByIDRequestBody$outboundSchema: z.ZodMiniType<RenderTemplateByIDRequestBody$Outbound, RenderTemplateByIDRequestBody>;
export declare function renderTemplateByIDRequestBodyToJSON(renderTemplateByIDRequestBody: RenderTemplateByIDRequestBody): string;
//# sourceMappingURL=rendertemplatebyidrequestbody.d.ts.map