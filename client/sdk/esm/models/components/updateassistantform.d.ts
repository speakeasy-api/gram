import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { AssistantMCPServerRef, AssistantMCPServerRef$Outbound } from "./assistantmcpserverref.js";
import { AssistantToolsetRef, AssistantToolsetRef$Outbound } from "./assistanttoolsetref.js";
/**
 * The assistant status.
 */
export declare const UpdateAssistantFormStatus: {
    readonly Active: "active";
    readonly Paused: "paused";
};
/**
 * The assistant status.
 */
export type UpdateAssistantFormStatus = ClosedEnum<typeof UpdateAssistantFormStatus>;
export type UpdateAssistantForm = {
    /**
     * The assistant ID.
     */
    id: string;
    /**
     * The system instructions for the assistant.
     */
    instructions?: string | undefined;
    /**
     * Maximum active warm runtimes.
     */
    maxConcurrency?: number | undefined;
    /**
     * MCP servers attached directly to the assistant (remote- or tunnelled-backed).
     */
    mcpServers?: Array<AssistantMCPServerRef> | undefined;
    /**
     * The model identifier used by the assistant.
     */
    model?: string | undefined;
    /**
     * The assistant name.
     */
    name?: string | undefined;
    /**
     * The assistant status.
     */
    status?: UpdateAssistantFormStatus | undefined;
    /**
     * Toolsets available to the assistant.
     */
    toolsets?: Array<AssistantToolsetRef> | undefined;
    /**
     * Warm runtime TTL in seconds.
     */
    warmTtlSeconds?: number | undefined;
};
/** @internal */
export declare const UpdateAssistantFormStatus$outboundSchema: z.ZodMiniEnum<typeof UpdateAssistantFormStatus>;
/** @internal */
export type UpdateAssistantForm$Outbound = {
    id: string;
    instructions?: string | undefined;
    max_concurrency?: number | undefined;
    mcp_servers?: Array<AssistantMCPServerRef$Outbound> | undefined;
    model?: string | undefined;
    name?: string | undefined;
    status?: string | undefined;
    toolsets?: Array<AssistantToolsetRef$Outbound> | undefined;
    warm_ttl_seconds?: number | undefined;
};
/** @internal */
export declare const UpdateAssistantForm$outboundSchema: z.ZodMiniType<UpdateAssistantForm$Outbound, UpdateAssistantForm>;
export declare function updateAssistantFormToJSON(updateAssistantForm: UpdateAssistantForm): string;
//# sourceMappingURL=updateassistantform.d.ts.map