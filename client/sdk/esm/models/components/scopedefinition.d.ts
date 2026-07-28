import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The scope used to store exception rules for this scope.
 */
export declare const ExclusionScope: {
  readonly OrgBlockedRead: "org:blocked_read";
  readonly OrgBlockedAdmin: "org:blocked_admin";
  readonly ProjectBlockedRead: "project:blocked_read";
  readonly ProjectBlockedWrite: "project:blocked_write";
  readonly McpBlockedRead: "mcp:blocked_read";
  readonly McpBlockedWrite: "mcp:blocked_write";
  readonly McpBlockedConnect: "mcp:blocked_connect";
  readonly EnvironmentBlockedRead: "environment:blocked_read";
  readonly EnvironmentBlockedWrite: "environment:blocked_write";
  readonly RiskPolicyBypass: "risk_policy:bypass";
};
/**
 * The scope used to store exception rules for this scope.
 */
export type ExclusionScope = ClosedEnum<typeof ExclusionScope>;
/**
 * The type of resource this scope applies to.
 */
export declare const ResourceType: {
  readonly Org: "org";
  readonly Project: "project";
  readonly Mcp: "mcp";
  readonly Environment: "environment";
  readonly RiskPolicy: "risk_policy";
  readonly Chat: "chat";
};
/**
 * The type of resource this scope applies to.
 */
export type ResourceType = ClosedEnum<typeof ResourceType>;
/**
 * Unique scope identifier.
 */
export declare const Slug: {
  readonly OrgRead: "org:read";
  readonly OrgBlockedRead: "org:blocked_read";
  readonly OrgAdmin: "org:admin";
  readonly OrgBlockedAdmin: "org:blocked_admin";
  readonly ProjectRead: "project:read";
  readonly ProjectBlockedRead: "project:blocked_read";
  readonly ProjectWrite: "project:write";
  readonly ProjectBlockedWrite: "project:blocked_write";
  readonly McpRead: "mcp:read";
  readonly McpBlockedRead: "mcp:blocked_read";
  readonly McpWrite: "mcp:write";
  readonly McpBlockedWrite: "mcp:blocked_write";
  readonly McpConnect: "mcp:connect";
  readonly McpBlockedConnect: "mcp:blocked_connect";
  readonly EnvironmentRead: "environment:read";
  readonly EnvironmentBlockedRead: "environment:blocked_read";
  readonly EnvironmentWrite: "environment:write";
  readonly EnvironmentBlockedWrite: "environment:blocked_write";
  readonly RiskPolicyEvaluate: "risk_policy:evaluate";
  readonly RiskPolicyBypass: "risk_policy:bypass";
  readonly ChatRead: "chat:read";
};
/**
 * Unique scope identifier.
 */
export type Slug = ClosedEnum<typeof Slug>;
/**
 * Whether this scope is a first-class permission or an internal storage/evaluation scope.
 */
export declare const Visibility: {
  readonly UserVisible: "user_visible";
  readonly Internal: "internal";
};
/**
 * Whether this scope is a first-class permission or an internal storage/evaluation scope.
 */
export type Visibility = ClosedEnum<typeof Visibility>;
export type ScopeDefinition = {
  /**
   * What this scope protects.
   */
  description: string;
  /**
   * The scope used to store exception rules for this scope.
   */
  exclusionScope?: ExclusionScope | undefined;
  /**
   * The type of resource this scope applies to.
   */
  resourceType: ResourceType;
  /**
   * Unique scope identifier.
   */
  slug: Slug;
  /**
   * Whether this scope is a first-class permission or an internal storage/evaluation scope.
   */
  visibility: Visibility;
};
/** @internal */
export declare const ExclusionScope$inboundSchema: z.ZodMiniEnum<
  typeof ExclusionScope
>;
/** @internal */
export declare const ResourceType$inboundSchema: z.ZodMiniEnum<
  typeof ResourceType
>;
/** @internal */
export declare const Slug$inboundSchema: z.ZodMiniEnum<typeof Slug>;
/** @internal */
export declare const Visibility$inboundSchema: z.ZodMiniEnum<typeof Visibility>;
/** @internal */
export declare const ScopeDefinition$inboundSchema: z.ZodMiniType<
  ScopeDefinition,
  unknown
>;
export declare function scopeDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<ScopeDefinition, SDKValidationError>;
//# sourceMappingURL=scopedefinition.d.ts.map
