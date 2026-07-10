import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The template engine
 */
export declare const CreatePromptTemplateFormEngine: {
    readonly Mustache: "mustache";
};
/**
 * The template engine
 */
export type CreatePromptTemplateFormEngine = ClosedEnum<typeof CreatePromptTemplateFormEngine>;
/**
 * The kind of prompt the template is used for
 */
export declare const CreatePromptTemplateFormKind: {
    readonly Prompt: "prompt";
    readonly HigherOrderTool: "higher_order_tool";
};
/**
 * The kind of prompt the template is used for
 */
export type CreatePromptTemplateFormKind = ClosedEnum<typeof CreatePromptTemplateFormKind>;
export type CreatePromptTemplateForm = {
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
    engine: CreatePromptTemplateFormEngine;
    /**
     * The kind of prompt the template is used for
     */
    kind: CreatePromptTemplateFormKind;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    name: string;
    /**
     * The template content
     */
    prompt: string;
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
export declare const CreatePromptTemplateFormEngine$outboundSchema: z.ZodMiniEnum<typeof CreatePromptTemplateFormEngine>;
/** @internal */
export declare const CreatePromptTemplateFormKind$outboundSchema: z.ZodMiniEnum<typeof CreatePromptTemplateFormKind>;
/** @internal */
export type CreatePromptTemplateForm$Outbound = {
    arguments?: string | undefined;
    description?: string | undefined;
    engine: string;
    kind: string;
    name: string;
    prompt: string;
    tool_urns_hint?: Array<string> | undefined;
    tools_hint?: Array<string> | undefined;
};
/** @internal */
export declare const CreatePromptTemplateForm$outboundSchema: z.ZodMiniType<CreatePromptTemplateForm$Outbound, CreatePromptTemplateForm>;
export declare function createPromptTemplateFormToJSON(createPromptTemplateForm: CreatePromptTemplateForm): string;
//# sourceMappingURL=createprompttemplateform.d.ts.map