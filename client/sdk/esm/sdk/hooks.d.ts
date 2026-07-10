import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ClaudeHookResult } from "../models/components/claudehookresult.js";
import { CodexHookResult } from "../models/components/codexhookresult.js";
import { CursorHookResult } from "../models/components/cursorhookresult.js";
import { IngestHookResult } from "../models/components/ingesthookresult.js";
import { HooksNumberClaudeRequest } from "../models/operations/hooksnumberclaude.js";
import { HooksNumberCodexRequest, HooksNumberCodexSecurity } from "../models/operations/hooksnumbercodex.js";
import { HooksNumberCursorRequest, HooksNumberCursorSecurity } from "../models/operations/hooksnumbercursor.js";
import { HooksNumberLogsRequest, HooksNumberLogsSecurity } from "../models/operations/hooksnumberlogs.js";
import { HooksNumberMetricsRequest, HooksNumberMetricsSecurity } from "../models/operations/hooksnumbermetrics.js";
import { IngestHookEventRequest, IngestHookEventSecurity } from "../models/operations/ingesthookevent.js";
export declare class Hooks extends ClientSDK {
    /**
     * claude hooks
     *
     * @remarks
     * Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.
     */
    hooksNumberClaude(request: HooksNumberClaudeRequest, options?: RequestOptions): Promise<ClaudeHookResult>;
    /**
     * codex hooks
     *
     * @remarks
     * Endpoint for Codex hook events. Handles SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, and Stop.
     */
    hooksNumberCodex(request: HooksNumberCodexRequest, security?: HooksNumberCodexSecurity | undefined, options?: RequestOptions): Promise<CodexHookResult>;
    /**
     * cursor hooks
     *
     * @remarks
     * Endpoint for Cursor hook events. Handles beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, and afterMCPExecution.
     */
    hooksNumberCursor(request: HooksNumberCursorRequest, security?: HooksNumberCursorSecurity | undefined, options?: RequestOptions): Promise<CursorHookResult>;
    /**
     * ingest hooks
     *
     * @remarks
     * Feature-first unified endpoint for hook events from supported coding assistants.
     */
    ingest(request: IngestHookEventRequest, security?: IngestHookEventSecurity | undefined, options?: RequestOptions): Promise<IngestHookResult>;
    /**
     * logs hooks
     *
     * @remarks
     * Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.
     */
    hooksNumberLogs(request: HooksNumberLogsRequest, security?: HooksNumberLogsSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * metrics hooks
     *
     * @remarks
     * Endpoint to receive OTEL metrics data from Claude Code. Requires API key authentication.
     */
    hooksNumberMetrics(request: HooksNumberMetricsRequest, security?: HooksNumberMetricsSecurity | undefined, options?: RequestOptions): Promise<void>;
}
//# sourceMappingURL=hooks.d.ts.map