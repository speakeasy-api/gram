import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool disposition filter (MCP scopes only).
 */
export declare const SelectorDisposition: {
  readonly ReadOnly: "read_only";
  readonly Destructive: "destructive";
  readonly Idempotent: "idempotent";
  readonly OpenWorld: "open_world";
};
/**
 * Tool disposition filter (MCP scopes only).
 */
export type SelectorDisposition = ClosedEnum<typeof SelectorDisposition>;
/**
 * The kind of resource this selector targets.
 */
export declare const ResourceKind: {
  readonly Project: "project";
  readonly Mcp: "mcp";
  readonly Org: "org";
  readonly Environment: "environment";
  readonly RiskPolicy: "risk_policy";
  readonly Chat: "chat";
  readonly Wildcard: "*";
};
/**
 * The kind of resource this selector targets.
 */
export type ResourceKind = ClosedEnum<typeof ResourceKind>;
/**
 * A constraint that narrows which resources a grant applies to.
 */
export type Selector = {
  /**
   * Tool disposition filter (MCP scopes only).
   */
  disposition?: SelectorDisposition | undefined;
  /**
   * Project filter (MCP scopes only). When set with resource_id='*', grants access to all servers in the project.
   */
  projectId?: string | undefined;
  /**
   * The resource identifier, or '*' for all resources of this kind.
   */
  resourceId: string;
  /**
   * The kind of resource this selector targets.
   */
  resourceKind: ResourceKind;
  /**
   * Server URL filter (risk policy scopes only). Include the URI scheme, for example https://api.example.com.
   */
  serverUrl?: string | undefined;
  /**
   * Specific tool name filter (MCP scopes only).
   */
  tool?: string | undefined;
};
/** @internal */
export declare const SelectorDisposition$inboundSchema: z.ZodMiniEnum<
  typeof SelectorDisposition
>;
/** @internal */
export declare const SelectorDisposition$outboundSchema: z.ZodMiniEnum<
  typeof SelectorDisposition
>;
/** @internal */
export declare const ResourceKind$inboundSchema: z.ZodMiniEnum<
  typeof ResourceKind
>;
/** @internal */
export declare const ResourceKind$outboundSchema: z.ZodMiniEnum<
  typeof ResourceKind
>;
/** @internal */
export declare const Selector$inboundSchema: z.ZodMiniType<Selector, unknown>;
/** @internal */
export type Selector$Outbound = {
  disposition?: string | undefined;
  project_id?: string | undefined;
  resource_id: string;
  resource_kind: string;
  server_url?: string | undefined;
  tool?: string | undefined;
};
/** @internal */
export declare const Selector$outboundSchema: z.ZodMiniType<
  Selector$Outbound,
  Selector
>;
export declare function selectorToJSON(selector: Selector): string;
export declare function selectorFromJSON(
  jsonString: string,
): SafeParseResult<Selector, SDKValidationError>;
//# sourceMappingURL=selector.d.ts.map
