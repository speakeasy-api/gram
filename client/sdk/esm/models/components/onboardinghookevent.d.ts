import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type OnboardingHookEvent = {
    /**
     * Gram chat/session ID that owns this event, when present.
     */
    chatId?: string | undefined;
    /**
     * Hook event name (e.g. PreToolUse, SessionStart).
     */
    eventName?: string | undefined;
    /**
     * Slug of the Gram project that received the event.
     */
    projectSlug: string;
    /**
     * Hook source: claude_code, cursor, or codex.
     */
    source: string;
    /**
     * Outcome status: allowed, blocked, failure, or pending.
     */
    status?: string | undefined;
    /**
     * Event timestamp in nanoseconds since unix epoch. Stringified to preserve int64 precision.
     */
    timeUnixNano: string;
    /**
     * Tool invoked by the hook, if any.
     */
    toolName?: string | undefined;
    /**
     * Email of the user whose session produced the event, when present in hook attributes.
     */
    userEmail?: string | undefined;
};
/** @internal */
export declare const OnboardingHookEvent$inboundSchema: z.ZodMiniType<OnboardingHookEvent, unknown>;
export declare function onboardingHookEventFromJSON(jsonString: string): SafeParseResult<OnboardingHookEvent, SDKValidationError>;
//# sourceMappingURL=onboardinghookevent.d.ts.map