import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CanonicalToolAttributes } from "./canonicaltoolattributes.js";
import { ToolAnnotations } from "./toolannotations.js";
import { ToolVariation } from "./toolvariation.js";
/**
 * The template engine
 */
export declare const Engine: {
    readonly Mustache: "mustache";
};
/**
 * The template engine
 */
export type Engine = ClosedEnum<typeof Engine>;
/**
 * The kind of prompt the template is used for
 */
export declare const PromptTemplateKind: {
    readonly Prompt: "prompt";
    readonly HigherOrderTool: "higher_order_tool";
};
/**
 * The kind of prompt the template is used for
 */
export type PromptTemplateKind = ClosedEnum<typeof PromptTemplateKind>;
/**
 * A prompt template
 */
export type PromptTemplate = {
    /**
     * Tool annotations providing behavioral hints about the tool
     */
    annotations?: ToolAnnotations | undefined;
    /**
     * The original details of a tool
     */
    canonical?: CanonicalToolAttributes | undefined;
    /**
     * The canonical name of the tool. Will be the same as the name if there is no variation.
     */
    canonicalName: string;
    /**
     * Confirmation mode for the tool
     */
    confirm?: string | undefined;
    /**
     * Prompt for the confirmation
     */
    confirmPrompt?: string | undefined;
    /**
     * The creation date of the tool.
     */
    createdAt: Date;
    /**
     * Description of the tool
     */
    description: string;
    /**
     * The template engine
     */
    engine: Engine;
    /**
     * The revision tree ID for the prompt template
     */
    historyId: string;
    /**
     * The ID of the tool
     */
    id: string;
    /**
     * The kind of prompt the template is used for
     */
    kind: PromptTemplateKind;
    /**
     * The name of the tool
     */
    name: string;
    /**
     * The previous version of the prompt template to use as predecessor
     */
    predecessorId?: string | undefined;
    /**
     * The ID of the project
     */
    projectId: string;
    /**
     * The template content
     */
    prompt: string;
    /**
     * JSON schema for the request
     */
    schema: string;
    /**
     * Version of the schema
     */
    schemaVersion?: string | undefined;
    /**
     * Summarizer for the tool
     */
    summarizer?: string | undefined;
    /**
     * The URN of this tool
     */
    toolUrn: string;
    /**
     * The suggested tool URNS associated with the prompt template
     */
    toolUrnsHint?: Array<string> | undefined;
    /**
     * The suggested tool names associated with the prompt template
     */
    toolsHint: Array<string>;
    /**
     * The last update date of the tool.
     */
    updatedAt: Date;
    variation?: ToolVariation | undefined;
};
/** @internal */
export declare const Engine$inboundSchema: z.ZodMiniEnum<typeof Engine>;
/** @internal */
export declare const PromptTemplateKind$inboundSchema: z.ZodMiniEnum<typeof PromptTemplateKind>;
/** @internal */
export declare const PromptTemplate$inboundSchema: z.ZodMiniType<PromptTemplate, unknown>;
export declare function promptTemplateFromJSON(jsonString: string): SafeParseResult<PromptTemplate, SDKValidationError>;
//# sourceMappingURL=prompttemplate.d.ts.map