import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AssistantMCPServerRef } from "./assistantmcpserverref.js";
import { AssistantToolsetRef } from "./assistanttoolsetref.js";
/**
 * The assistant status.
 */
export declare const AssistantStatus: {
  readonly Active: "active";
  readonly Paused: "paused";
};
/**
 * The assistant status.
 */
export type AssistantStatus = ClosedEnum<typeof AssistantStatus>;
export type Assistant = {
  /**
   * Creation timestamp.
   */
  createdAt: Date;
  /**
   * The ID of the user who created the assistant, if known.
   */
  createdByUserId?: string | undefined;
  /**
   * The assistant ID.
   */
  id: string;
  /**
   * The system instructions for the assistant.
   */
  instructions: string;
  /**
   * Maximum active warm runtimes for the assistant.
   */
  maxConcurrency: number;
  /**
   * MCP servers attached directly to the assistant (remote- or tunnelled-backed).
   */
  mcpServers: Array<AssistantMCPServerRef>;
  /**
   * The model identifier used by the assistant.
   */
  model: string;
  /**
   * The assistant name.
   */
  name: string;
  /**
   * The project ID owning the assistant.
   */
  projectId: string;
  /**
   * The assistant status.
   */
  status: AssistantStatus;
  /**
   * Toolsets available to the assistant.
   */
  toolsets: Array<AssistantToolsetRef>;
  /**
   * Last update timestamp.
   */
  updatedAt: Date;
  /**
   * Warm runtime TTL in seconds.
   */
  warmTtlSeconds: number;
};
/** @internal */
export declare const AssistantStatus$inboundSchema: z.ZodMiniEnum<
  typeof AssistantStatus
>;
/** @internal */
export declare const Assistant$inboundSchema: z.ZodMiniType<Assistant, unknown>;
export declare function assistantFromJSON(
  jsonString: string,
): SafeParseResult<Assistant, SDKValidationError>;
//# sourceMappingURL=assistant.d.ts.map
