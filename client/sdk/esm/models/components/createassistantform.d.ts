import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { AssistantMCPServerRef, AssistantMCPServerRef$Outbound } from "./assistantmcpserverref.js";
import { AssistantToolsetRef, AssistantToolsetRef$Outbound } from "./assistanttoolsetref.js";
/**
 * Optional initial status.
 */
export declare const CreateAssistantFormStatus: {
    readonly Active: "active";
    readonly Paused: "paused";
};
/**
 * Optional initial status.
 */
export type CreateAssistantFormStatus = ClosedEnum<typeof CreateAssistantFormStatus>;
export type CreateAssistantForm = {
    /**
     * The system instructions for the assistant.
     */
    instructions: string;
    /**
     * Optional maximum active warm runtimes.
     */
    maxConcurrency?: number | undefined;
    /**
     * MCP servers attached directly to the assistant (remote- or tunnelled-backed).
     */
    mcpServers?: Array<AssistantMCPServerRef> | undefined;
    /**
     * The model identifier used by the assistant.
     */
    model: string;
    /**
     * The assistant name.
     */
    name: string;
    /**
     * Optional initial status.
     */
    status?: CreateAssistantFormStatus | undefined;
    /**
     * Toolsets available to the assistant.
     */
    toolsets: Array<AssistantToolsetRef>;
    /**
     * Optional warm runtime TTL in seconds.
     */
    warmTtlSeconds?: number | undefined;
};
/** @internal */
export declare const CreateAssistantFormStatus$outboundSchema: z.ZodMiniEnum<typeof CreateAssistantFormStatus>;
/** @internal */
export type CreateAssistantForm$Outbound = {
    instructions: string;
    max_concurrency?: number | undefined;
    mcp_servers?: Array<AssistantMCPServerRef$Outbound> | undefined;
    model: string;
    name: string;
    status?: string | undefined;
    toolsets: Array<AssistantToolsetRef$Outbound>;
    warm_ttl_seconds?: number | undefined;
};
/** @internal */
export declare const CreateAssistantForm$outboundSchema: z.ZodMiniType<CreateAssistantForm$Outbound, CreateAssistantForm>;
export declare function createAssistantFormToJSON(createAssistantForm: CreateAssistantForm): string;
//# sourceMappingURL=createassistantform.d.ts.map