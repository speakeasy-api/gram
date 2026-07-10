import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Workflow state: running, sleeping, or not_started.
 */
export declare const WorkflowStatus: {
  readonly Running: "running";
  readonly Sleeping: "sleeping";
  readonly NotStarted: "not_started";
};
/**
 * Workflow state: running, sleeping, or not_started.
 */
export type WorkflowStatus = ClosedEnum<typeof WorkflowStatus>;
export type RiskPolicyStatus = {
  /**
   * Messages analyzed at the current policy version.
   */
  analyzedMessages: number;
  /**
   * Number of findings at the current policy version.
   */
  findingsCount: number;
  /**
   * Messages not yet analyzed.
   */
  pendingMessages: number;
  /**
   * The risk policy ID.
   */
  policyId: string;
  /**
   * Current policy version.
   */
  policyVersion: number;
  /**
   * Total messages in the project.
   */
  totalMessages: number;
  /**
   * Workflow state: running, sleeping, or not_started.
   */
  workflowStatus: WorkflowStatus;
};
/** @internal */
export declare const WorkflowStatus$inboundSchema: z.ZodMiniEnum<
  typeof WorkflowStatus
>;
/** @internal */
export declare const RiskPolicyStatus$inboundSchema: z.ZodMiniType<
  RiskPolicyStatus,
  unknown
>;
export declare function riskPolicyStatusFromJSON(
  jsonString: string,
): SafeParseResult<RiskPolicyStatus, SDKValidationError>;
//# sourceMappingURL=riskpolicystatus.d.ts.map
