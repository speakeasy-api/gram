import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The confirmation mode for the tool variation
 */
export declare const Confirm: {
    readonly Always: "always";
    readonly Never: "never";
    readonly Session: "session";
};
/**
 * The confirmation mode for the tool variation
 */
export type Confirm = ClosedEnum<typeof Confirm>;
export type UpsertGlobalToolVariationForm = {
    /**
     * The confirmation mode for the tool variation
     */
    confirm?: Confirm | undefined;
    /**
     * The confirmation prompt for the tool variation
     */
    confirmPrompt?: string | undefined;
    /**
     * The description of the tool variation
     */
    description?: string | undefined;
    /**
     * Override: if true, the tool may perform destructive updates
     */
    destructiveHint?: boolean | undefined;
    /**
     * Override: if true, repeated calls have no additional effect
     */
    idempotentHint?: boolean | undefined;
    /**
     * The name of the tool variation
     */
    name?: string | undefined;
    /**
     * Override: if true, the tool interacts with external entities
     */
    openWorldHint?: boolean | undefined;
    /**
     * Override: if true, the tool does not modify its environment
     */
    readOnlyHint?: boolean | undefined;
    /**
     * The name of the source tool
     */
    srcToolName: string;
    /**
     * The URN of the source tool
     */
    srcToolUrn: string;
    /**
     * The summarizer of the tool variation
     */
    summarizer?: string | undefined;
    /**
     * The summary of the tool variation
     */
    summary?: string | undefined;
    /**
     * The tags of the tool variation
     */
    tags?: Array<string> | undefined;
    /**
     * Display name override for the tool
     */
    title?: string | undefined;
};
/** @internal */
export declare const Confirm$outboundSchema: z.ZodMiniEnum<typeof Confirm>;
/** @internal */
export type UpsertGlobalToolVariationForm$Outbound = {
    confirm?: string | undefined;
    confirm_prompt?: string | undefined;
    description?: string | undefined;
    destructive_hint?: boolean | undefined;
    idempotent_hint?: boolean | undefined;
    name?: string | undefined;
    open_world_hint?: boolean | undefined;
    read_only_hint?: boolean | undefined;
    src_tool_name: string;
    src_tool_urn: string;
    summarizer?: string | undefined;
    summary?: string | undefined;
    tags?: Array<string> | undefined;
    title?: string | undefined;
};
/** @internal */
export declare const UpsertGlobalToolVariationForm$outboundSchema: z.ZodMiniType<UpsertGlobalToolVariationForm$Outbound, UpsertGlobalToolVariationForm>;
export declare function upsertGlobalToolVariationFormToJSON(upsertGlobalToolVariationForm: UpsertGlobalToolVariationForm): string;
//# sourceMappingURL=upsertglobaltoolvariationform.d.ts.map