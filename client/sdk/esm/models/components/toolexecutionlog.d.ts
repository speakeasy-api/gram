import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Structured log entry from a tool execution
 */
export type ToolExecutionLog = {
  /**
   * JSON-encoded log attributes
   */
  attributes?: string | undefined;
  /**
   * Deployment UUID
   */
  deploymentId: string;
  /**
   * Function UUID
   */
  functionId: string;
  /**
   * Log entry ID
   */
  id: string;
  /**
   * Instance identifier
   */
  instance: string;
  /**
   * Log level
   */
  level: string;
  /**
   * Parsed log message
   */
  message?: string | undefined;
  /**
   * Project UUID
   */
  projectId: string;
  /**
   * Raw log message
   */
  rawLog: string;
  /**
   * Log source
   */
  source: string;
  /**
   * Timestamp of the log entry
   */
  timestamp: Date;
};
/** @internal */
export declare const ToolExecutionLog$inboundSchema: z.ZodType<
  ToolExecutionLog,
  z.ZodTypeDef,
  unknown
>;
export declare function toolExecutionLogFromJSON(
  jsonString: string,
): SafeParseResult<ToolExecutionLog, SDKValidationError>;
//# sourceMappingURL=toolexecutionlog.d.ts.map
