import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The template engine
 */
export declare const RenderTemplateRequestBodyEngine: {
    readonly Mustache: "mustache";
};
/**
 * The template engine
 */
export type RenderTemplateRequestBodyEngine = ClosedEnum<typeof RenderTemplateRequestBodyEngine>;
/**
 * The kind of prompt the template is used for
 */
export declare const RenderTemplateRequestBodyKind: {
    readonly Prompt: "prompt";
    readonly HigherOrderTool: "higher_order_tool";
};
/**
 * The kind of prompt the template is used for
 */
export type RenderTemplateRequestBodyKind = ClosedEnum<typeof RenderTemplateRequestBodyKind>;
export type RenderTemplateRequestBody = {
    /**
     * The input data to render the template with
     */
    arguments: {
        [k: string]: any;
    };
    /**
     * The template engine
     */
    engine: RenderTemplateRequestBodyEngine;
    /**
     * The kind of prompt the template is used for
     */
    kind: RenderTemplateRequestBodyKind;
    /**
     * The template content to render
     */
    prompt: string;
};
/** @internal */
export declare const RenderTemplateRequestBodyEngine$outboundSchema: z.ZodMiniEnum<typeof RenderTemplateRequestBodyEngine>;
/** @internal */
export declare const RenderTemplateRequestBodyKind$outboundSchema: z.ZodMiniEnum<typeof RenderTemplateRequestBodyKind>;
/** @internal */
export type RenderTemplateRequestBody$Outbound = {
    arguments: {
        [k: string]: any;
    };
    engine: string;
    kind: string;
    prompt: string;
};
/** @internal */
export declare const RenderTemplateRequestBody$outboundSchema: z.ZodMiniType<RenderTemplateRequestBody$Outbound, RenderTemplateRequestBody>;
export declare function renderTemplateRequestBodyToJSON(renderTemplateRequestBody: RenderTemplateRequestBody): string;
//# sourceMappingURL=rendertemplaterequestbody.d.ts.map