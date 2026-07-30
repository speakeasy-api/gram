import * as z from "zod/v3";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Type of tool being logged
 */
export declare const ToolType: {
  readonly Http: "http";
  readonly Function: "function";
  readonly Prompt: "prompt";
};
/**
 * Type of tool being logged
 */
export type ToolType = ClosedEnum<typeof ToolType>;
/**
 * HTTP tool request and response log entry
 */
export type HTTPToolLog = {
  /**
   * Deployment UUID
   */
  deploymentId: string;
  /**
   * Duration in milliseconds
   */
  durationMs: number;
  /**
   * HTTP method
   */
  httpMethod: string;
  /**
   * HTTP route
   */
  httpRoute: string;
  /**
   * HTTP Server URL
   */
  httpServerUrl: string;
  /**
   * Id of the request
   */
  id?: string | undefined;
  /**
   * Organization UUID
   */
  organizationId: string;
  /**
   * Project UUID
   */
  projectId?: string | undefined;
  /**
   * Request body size in bytes
   */
  requestBodyBytes?: number | undefined;
  /**
   * Request headers
   */
  requestHeaders?:
    | {
        [k: string]: string;
      }
    | undefined;
  /**
   * Response body size in bytes
   */
  responseBodyBytes?: number | undefined;
  /**
   * Response headers
   */
  responseHeaders?:
    | {
        [k: string]: string;
      }
    | undefined;
  /**
   * Span ID for correlation
   */
  spanId: string;
  /**
   * HTTP status code
   */
  statusCode: number;
  /**
   * Tool UUID
   */
  toolId: string;
  /**
   * Type of tool being logged
   */
  toolType: ToolType;
  /**
   * Tool URN
   */
  toolUrn: string;
  /**
   * Trace ID for correlation
   */
  traceId: string;
  /**
   * Timestamp of the request
   */
  ts: Date;
  /**
   * User agent
   */
  userAgent: string;
};
/** @internal */
export declare const ToolType$inboundSchema: z.ZodNativeEnum<typeof ToolType>;
/** @internal */
export declare const HTTPToolLog$inboundSchema: z.ZodType<
  HTTPToolLog,
  z.ZodTypeDef,
  unknown
>;
export declare function httpToolLogFromJSON(
  jsonString: string,
): SafeParseResult<HTTPToolLog, SDKValidationError>;
//# sourceMappingURL=httptoollog.d.ts.map
