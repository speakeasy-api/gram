import * as z from "zod/v4-mini";
import { HookMCPData, HookMCPData$Outbound } from "./hookmcpdata.js";
import { HookMessageData, HookMessageData$Outbound } from "./hookmessagedata.js";
import { HookNotificationData, HookNotificationData$Outbound } from "./hooknotificationdata.js";
import { HookPromptData, HookPromptData$Outbound } from "./hookpromptdata.js";
import { HookSkillData, HookSkillData$Outbound } from "./hookskilldata.js";
import { HookToolCallData, HookToolCallData$Outbound } from "./hooktoolcalldata.js";
import { HookUsageData, HookUsageData$Outbound } from "./hookusagedata.js";
/**
 * Feature-specific payloads. Hooks populate only the blocks needed for the event.
 */
export type HookIngestData = {
    /**
     * MCP feature payload.
     */
    mcp?: HookMCPData | undefined;
    /**
     * Assistant/user message payload.
     */
    message?: HookMessageData | undefined;
    /**
     * Local agent notification payload.
     */
    notification?: HookNotificationData | undefined;
    /**
     * Prompt feature payload.
     */
    prompt?: HookPromptData | undefined;
    /**
     * Skill activation payload.
     */
    skill?: HookSkillData | undefined;
    /**
     * Tool call feature payload.
     */
    toolCall?: HookToolCallData | undefined;
    /**
     * Token and cost usage payload.
     */
    usage?: HookUsageData | undefined;
};
/** @internal */
export type HookIngestData$Outbound = {
    mcp?: HookMCPData$Outbound | undefined;
    message?: HookMessageData$Outbound | undefined;
    notification?: HookNotificationData$Outbound | undefined;
    prompt?: HookPromptData$Outbound | undefined;
    skill?: HookSkillData$Outbound | undefined;
    tool_call?: HookToolCallData$Outbound | undefined;
    usage?: HookUsageData$Outbound | undefined;
};
/** @internal */
export declare const HookIngestData$outboundSchema: z.ZodMiniType<HookIngestData$Outbound, HookIngestData>;
export declare function hookIngestDataToJSON(hookIngestData: HookIngestData): string;
//# sourceMappingURL=hookingestdata.d.ts.map