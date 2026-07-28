import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Server classification, present for MCP server nodes
 */
export declare const ServerClass: {
  readonly Gram: "gram";
  readonly External: "external";
  readonly Local: "local";
};
/**
 * Server classification, present for MCP server nodes
 */
export type ServerClass = ClosedEnum<typeof ServerClass>;
/**
 * Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL.
 */
export declare const Tier: {
  readonly Origin: "origin";
  readonly Client: "client";
  readonly Server: "server";
  readonly Tool: "tool";
};
/**
 * Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL.
 */
export type Tier = ClosedEnum<typeof Tier>;
/**
 * A node in the employee data flow graph
 */
export type EmployeeDataFlowNode = {
  /**
   * Stable node ID
   */
  id: string;
  /**
   * Display label
   */
  label: string;
  /**
   * Server classification, present for MCP server nodes
   */
  serverClass?: ServerClass | undefined;
  /**
   * Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL.
   */
  tier: Tier;
  /**
   * Total calls involving this node
   */
  totalCalls: number;
};
/** @internal */
export declare const ServerClass$inboundSchema: z.ZodMiniEnum<
  typeof ServerClass
>;
/** @internal */
export declare const Tier$inboundSchema: z.ZodMiniEnum<typeof Tier>;
/** @internal */
export declare const EmployeeDataFlowNode$inboundSchema: z.ZodMiniType<
  EmployeeDataFlowNode,
  unknown
>;
export declare function employeeDataFlowNodeFromJSON(
  jsonString: string,
): SafeParseResult<EmployeeDataFlowNode, SDKValidationError>;
//# sourceMappingURL=employeedataflownode.d.ts.map
