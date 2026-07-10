import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The template engine
 */
export declare const UpdatePromptTemplateFormEngine: {
    readonly Mustache: "mustache";
};
/**
 * The template engine
 */
export type UpdatePromptTemplateFormEngine = ClosedEnum<typeof UpdatePromptTemplateFormEngine>;
/**
 * The kind of prompt the template is used for
 */
export declare const UpdatePromptTemplateFormKind: {
    readonly Prompt: "prompt";
    readonly HigherOrderTool: "higher_order_tool";
};
/**
 * The kind of prompt the template is used for
 */
export type UpdatePromptTemplateFormKind = ClosedEnum<typeof UpdatePromptTemplateFormKind>;
export type UpdatePromptTemplateForm = {
    /**
     * The JSON Schema defining the placeholders found in the prompt template
     */
    arguments?: string | undefined;
    /**
     * The description of the prompt template
     */
    description?: string | undefined;
    /**
     * The template engine
     */
    engine?: UpdatePromptTemplateFormEngine | undefined;
    /**
     * The ID of the prompt template to update
     */
    id: string;
    /**
     * The kind of prompt the template is used for
     */
    kind?: UpdatePromptTemplateFormKind | undefined;
    /**
     * The name of the prompt template. Will be updated via variation
     */
    name?: string | undefined;
    /**
     * The template content
     */
    prompt?: string | undefined;
    /**
     * The suggested tool URNS associated with the prompt template
     */
    toolUrnsHint?: Array<string> | undefined;
    /**
     * The suggested tool names associated with the prompt template
     */
    toolsHint?: Array<string> | undefined;
};
/** @internal */
export declare const UpdatePromptTemplateFormEngine$outboundSchema: z.ZodMiniEnum<typeof UpdatePromptTemplateFormEngine>;
/** @internal */
export declare const UpdatePromptTemplateFormKind$outboundSchema: z.ZodMiniEnum<typeof UpdatePromptTemplateFormKind>;
/** @internal */
export type UpdatePromptTemplateForm$Outbound = {
    arguments?: string | undefined;
    description?: string | undefined;
    engine?: string | undefined;
    id: string;
    kind?: string | undefined;
    name?: string | undefined;
    prompt?: string | undefined;
    tool_urns_hint?: Array<string> | undefined;
    tools_hint?: Array<string> | undefined;
};
/** @internal */
export declare const UpdatePromptTemplateForm$outboundSchema: z.ZodMiniType<UpdatePromptTemplateForm$Outbound, UpdatePromptTemplateForm>;
export declare function updatePromptTemplateFormToJSON(updatePromptTemplateForm: UpdatePromptTemplateForm): string;
//# sourceMappingURL=updateprompttemplateform.d.ts.map