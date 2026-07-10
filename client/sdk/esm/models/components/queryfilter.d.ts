import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Dimension to filter on
 */
export declare const Dimension: {
  readonly DepartmentName: "department_name";
  readonly JobTitle: "job_title";
  readonly EmployeeType: "employee_type";
  readonly DivisionName: "division_name";
  readonly CostCenterName: "cost_center_name";
  readonly Email: "email";
  readonly Model: "model";
  readonly HookSource: "hook_source";
  readonly AccountType: "account_type";
  readonly Provider: "provider";
  readonly BillingMode: "billing_mode";
  readonly QuerySource: "query_source";
  readonly SkillName: "skill_name";
  readonly AgentName: "agent_name";
  readonly McpServerName: "mcp_server_name";
  readonly McpToolName: "mcp_tool_name";
  readonly Role: "role";
  readonly Group: "group";
  readonly ProjectId: "project_id";
};
/**
 * Dimension to filter on
 */
export type Dimension = ClosedEnum<typeof Dimension>;
/**
 * A single filter predicate on an allowlisted dimension
 */
export type QueryFilter = {
  /**
   * Dimension to filter on
   */
  dimension: Dimension;
  /**
   * Match if the dimension equals any of these values (IN semantics; for multi-valued dimensions like role/group, matches if any element is present).
   */
  values: Array<string>;
};
/** @internal */
export declare const Dimension$outboundSchema: z.ZodMiniEnum<typeof Dimension>;
/** @internal */
export type QueryFilter$Outbound = {
  dimension: string;
  values: Array<string>;
};
/** @internal */
export declare const QueryFilter$outboundSchema: z.ZodMiniType<
  QueryFilter$Outbound,
  QueryFilter
>;
export declare function queryFilterToJSON(queryFilter: QueryFilter): string;
//# sourceMappingURL=queryfilter.d.ts.map
