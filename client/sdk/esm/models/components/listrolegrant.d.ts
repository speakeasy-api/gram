import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Selector } from "./selector.js";
/**
 * The scope slug this grant applies to.
 */
export declare const ListRoleGrantScope: {
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
 * The scope slug this grant applies to.
 */
export type ListRoleGrantScope = ClosedEnum<typeof ListRoleGrantScope>;
export declare const SubScopes: {
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
export type SubScopes = ClosedEnum<typeof SubScopes>;
export type ListRoleGrant = {
    /**
     * The scope slug this grant applies to.
     */
    scope: ListRoleGrantScope;
    /**
     * Selector constraints. Null means unrestricted.
     */
    selectors?: Array<Selector> | undefined;
    /**
     * The inherited scopes the primary scope grants.
     */
    subScopes?: Array<SubScopes> | undefined;
};
/** @internal */
export declare const ListRoleGrantScope$inboundSchema: z.ZodMiniEnum<typeof ListRoleGrantScope>;
/** @internal */
export declare const SubScopes$inboundSchema: z.ZodMiniEnum<typeof SubScopes>;
/** @internal */
export declare const ListRoleGrant$inboundSchema: z.ZodMiniType<ListRoleGrant, unknown>;
export declare function listRoleGrantFromJSON(jsonString: string): SafeParseResult<ListRoleGrant, SDKValidationError>;
//# sourceMappingURL=listrolegrant.d.ts.map