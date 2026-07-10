import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Canonical Gram hook event type.
 */
export declare const HookIngestEventType: {
    readonly SessionStarted: "session.started";
    readonly SessionUpdated: "session.updated";
    readonly SessionEnded: "session.ended";
    readonly PromptSubmitted: "prompt.submitted";
    readonly ToolRequested: "tool.requested";
    readonly ToolCompleted: "tool.completed";
    readonly ToolFailed: "tool.failed";
    readonly AssistantResponded: "assistant.responded";
    readonly AssistantThought: "assistant.thought";
    readonly UsageReported: "usage.reported";
    readonly SkillActivated: "skill.activated";
    readonly NotificationReported: "notification.reported";
};
/**
 * Canonical Gram hook event type.
 */
export type HookIngestEventType = ClosedEnum<typeof HookIngestEventType>;
/**
 * Canonical Gram feature event.
 */
export type HookIngestEvent = {
    /**
     * RFC3339 timestamp from the local agent. Defaults to receive time when absent.
     */
    occurredAt?: Date | undefined;
    /**
     * Canonical Gram hook event type.
     */
    type: HookIngestEventType;
};
/** @internal */
export declare const HookIngestEventType$outboundSchema: z.ZodMiniEnum<typeof HookIngestEventType>;
/** @internal */
export type HookIngestEvent$Outbound = {
    occurred_at?: string | undefined;
    type: string;
};
/** @internal */
export declare const HookIngestEvent$outboundSchema: z.ZodMiniType<HookIngestEvent$Outbound, HookIngestEvent>;
export declare function hookIngestEventToJSON(hookIngestEvent: HookIngestEvent): string;
//# sourceMappingURL=hookingestevent.d.ts.map