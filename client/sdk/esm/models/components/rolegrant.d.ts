import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Selector, Selector$Outbound } from "./selector.js";
/**
 * The scope slug this grant applies to.
 */
export declare const Scope: {
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
export type Scope = ClosedEnum<typeof Scope>;
export type RoleGrant = {
    /**
     * The scope slug this grant applies to.
     */
    scope: Scope;
    /**
     * Selector constraints. Null means unrestricted.
     */
    selectors?: Array<Selector> | undefined;
};
/** @internal */
export declare const Scope$inboundSchema: z.ZodMiniEnum<typeof Scope>;
/** @internal */
export declare const Scope$outboundSchema: z.ZodMiniEnum<typeof Scope>;
/** @internal */
export declare const RoleGrant$inboundSchema: z.ZodMiniType<RoleGrant, unknown>;
/** @internal */
export type RoleGrant$Outbound = {
    scope: string;
    selectors?: Array<Selector$Outbound> | undefined;
};
/** @internal */
export declare const RoleGrant$outboundSchema: z.ZodMiniType<RoleGrant$Outbound, RoleGrant>;
export declare function roleGrantToJSON(roleGrant: RoleGrant): string;
export declare function roleGrantFromJSON(jsonString: string): SafeParseResult<RoleGrant, SDKValidationError>;
//# sourceMappingURL=rolegrant.d.ts.map