import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AccessMember } from "../models/components/accessmember.js";
import { CreateShadowMCPAccessRuleResult } from "../models/components/createshadowmcpaccessruleresult.js";
import { ListChallengeBucketsResult } from "../models/components/listchallengebucketsresult.js";
import { ListChallengesResult } from "../models/components/listchallengesresult.js";
import { ListMembersResult } from "../models/components/listmembersresult.js";
import { ListRolesResult } from "../models/components/listrolesresult.js";
import { ListScopesResult } from "../models/components/listscopesresult.js";
import { ListShadowMCPAccessRulesResult } from "../models/components/listshadowmcpaccessrulesresult.js";
import { ListShadowMCPApprovalRequestsResult } from "../models/components/listshadowmcpapprovalrequestsresult.js";
import { ListUserGrantsResult } from "../models/components/listusergrantsresult.js";
import { RBACStatus } from "../models/components/rbacstatus.js";
import { ResolveChallengesResult } from "../models/components/resolvechallengesresult.js";
import { Role } from "../models/components/role.js";
import { ShadowMCPAccessRule } from "../models/components/shadowmcpaccessrule.js";
import { ShadowMCPApprovalDecisionResult } from "../models/components/shadowmcpapprovaldecisionresult.js";
import { ShadowMCPApprovalRequest } from "../models/components/shadowmcpapprovalrequest.js";
import {
  ApproveShadowMCPApprovalRequestRequest,
  ApproveShadowMCPApprovalRequestSecurity,
} from "../models/operations/approveshadowmcpapprovalrequest.js";
import {
  CreateRoleRequest,
  CreateRoleSecurity,
} from "../models/operations/createrole.js";
import {
  CreateShadowMCPAccessRuleRequest,
  CreateShadowMCPAccessRuleSecurity,
} from "../models/operations/createshadowmcpaccessrule.js";
import {
  CreateShadowMCPApprovalRequestRequest,
  CreateShadowMCPApprovalRequestSecurity,
} from "../models/operations/createshadowmcpapprovalrequest.js";
import {
  DeleteRoleRequest,
  DeleteRoleSecurity,
} from "../models/operations/deleterole.js";
import {
  DeleteShadowMCPAccessRuleRequest,
  DeleteShadowMCPAccessRuleSecurity,
} from "../models/operations/deleteshadowmcpaccessrule.js";
import {
  DenyShadowMCPApprovalRequestRequest,
  DenyShadowMCPApprovalRequestSecurity,
} from "../models/operations/denyshadowmcpapprovalrequest.js";
import {
  DisableRBACRequest,
  DisableRBACSecurity,
} from "../models/operations/disablerbac.js";
import {
  EnableRBACRequest,
  EnableRBACSecurity,
} from "../models/operations/enablerbac.js";
import {
  GetRBACStatusRequest,
  GetRBACStatusSecurity,
} from "../models/operations/getrbacstatus.js";
import {
  GetRoleRequest,
  GetRoleSecurity,
} from "../models/operations/getrole.js";
import {
  ListChallengeBucketsRequest,
  ListChallengeBucketsSecurity,
} from "../models/operations/listchallengebuckets.js";
import {
  ListChallengesRequest,
  ListChallengesSecurity,
} from "../models/operations/listchallenges.js";
import {
  ListGrantsRequest,
  ListGrantsSecurity,
} from "../models/operations/listgrants.js";
import {
  ListMembersRequest,
  ListMembersSecurity,
} from "../models/operations/listmembers.js";
import {
  ListRolesRequest,
  ListRolesSecurity,
} from "../models/operations/listroles.js";
import {
  ListScopesRequest,
  ListScopesSecurity,
} from "../models/operations/listscopes.js";
import {
  ListShadowMCPAccessRulesRequest,
  ListShadowMCPAccessRulesSecurity,
} from "../models/operations/listshadowmcpaccessrules.js";
import {
  ListShadowMCPApprovalRequestsRequest,
  ListShadowMCPApprovalRequestsSecurity,
} from "../models/operations/listshadowmcpapprovalrequests.js";
import {
  ResolveChallengeRequest,
  ResolveChallengeSecurity,
} from "../models/operations/resolvechallenge.js";
import {
  UpdateMemberRolesRequest,
  UpdateMemberRolesSecurity,
} from "../models/operations/updatememberroles.js";
import {
  UpdateRoleRequest,
  UpdateRoleSecurity,
} from "../models/operations/updaterole.js";
import {
  UpdateShadowMCPAccessRuleRequest,
  UpdateShadowMCPAccessRuleSecurity,
} from "../models/operations/updateshadowmcpaccessrule.js";
export declare class Access extends ClientSDK {
  /**
   * approveShadowMCPApprovalRequest access
   *
   * @remarks
   * Approve a Shadow MCP request, creating an allow rule scoped to the organization or project.
   */
  approveShadowMCPApprovalRequest(
    request: ApproveShadowMCPApprovalRequestRequest,
    security?: ApproveShadowMCPApprovalRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ShadowMCPApprovalDecisionResult>;
  /**
   * createRole access
   *
   * @remarks
   * Create a new custom role.
   */
  createRole(
    request: CreateRoleRequest,
    security?: CreateRoleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Role>;
  /**
   * createShadowMCPApprovalRequest access
   *
   * @remarks
   * Create or return an active Shadow MCP approval request.
   */
  createShadowMCPApprovalRequest(
    request: CreateShadowMCPApprovalRequestRequest,
    security?: CreateShadowMCPApprovalRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ShadowMCPApprovalRequest>;
  /**
   * createShadowMCPAccessRule access
   *
   * @remarks
   * Create a managed Shadow MCP access rule.
   */
  createShadowMCPAccessRule(
    request: CreateShadowMCPAccessRuleRequest,
    security?: CreateShadowMCPAccessRuleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreateShadowMCPAccessRuleResult>;
  /**
   * deleteRole access
   *
   * @remarks
   * Delete a custom role (system roles cannot be deleted).
   */
  deleteRole(
    request: DeleteRoleRequest,
    security?: DeleteRoleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * deleteShadowMCPAccessRule access
   *
   * @remarks
   * Delete a managed Shadow MCP access rule.
   */
  deleteShadowMCPAccessRule(
    request: DeleteShadowMCPAccessRuleRequest,
    security?: DeleteShadowMCPAccessRuleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * denyShadowMCPApprovalRequest access
   *
   * @remarks
   * Deny a Shadow MCP request and optionally create a deny rule.
   */
  denyShadowMCPApprovalRequest(
    request: DenyShadowMCPApprovalRequestRequest,
    security?: DenyShadowMCPApprovalRequestSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ShadowMCPApprovalDecisionResult>;
  /**
   * disableRBAC access
   *
   * @remarks
   * Disable RBAC enforcement for the current organization.
   */
  disableRBAC(
    request?: DisableRBACRequest | undefined,
    security?: DisableRBACSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * enableRBAC access
   *
   * @remarks
   * Enable RBAC for the current organization. Seeds default grants for system roles.
   */
  enableRBAC(
    request?: EnableRBACRequest | undefined,
    security?: EnableRBACSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getRBACStatus access
   *
   * @remarks
   * Returns whether RBAC is currently enabled for the current organization.
   */
  getRBACStatus(
    request?: GetRBACStatusRequest | undefined,
    security?: GetRBACStatusSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RBACStatus>;
  /**
   * getRole access
   *
   * @remarks
   * Get a role by ID.
   */
  getRole(
    request: GetRoleRequest,
    security?: GetRoleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Role>;
  /**
   * listChallengeBuckets access
   *
   * @remarks
   * List authz challenges grouped into time-based burst buckets. Consecutive challenges with the same dimensions within a 10-minute window are collapsed into a single bucket.
   */
  listChallengeBuckets(
    request?: ListChallengeBucketsRequest | undefined,
    security?: ListChallengeBucketsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListChallengeBucketsResult>;
  /**
   * listChallenges access
   *
   * @remarks
   * List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.
   */
  listChallenges(
    request?: ListChallengesRequest | undefined,
    security?: ListChallengesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListChallengesResult>;
  /**
   * listGrants access
   *
   * @remarks
   * List the current user's effective grants, including inherited role grants.
   */
  listGrants(
    request?: ListGrantsRequest | undefined,
    security?: ListGrantsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListUserGrantsResult>;
  /**
   * listMembers access
   *
   * @remarks
   * List all team members with their role assignments.
   */
  listMembers(
    request?: ListMembersRequest | undefined,
    security?: ListMembersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListMembersResult>;
  /**
   * listRoles access
   *
   * @remarks
   * List all roles for the current organization.
   */
  listRoles(
    request?: ListRolesRequest | undefined,
    security?: ListRolesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListRolesResult>;
  /**
   * listScopes access
   *
   * @remarks
   * List all available scopes and their resource types.
   */
  listScopes(
    request?: ListScopesRequest | undefined,
    security?: ListScopesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListScopesResult>;
  /**
   * listShadowMCPApprovalRequests access
   *
   * @remarks
   * List Shadow MCP approval requests for the current organization. Requires organization admin access because requests include requester and block details.
   */
  listShadowMCPApprovalRequests(
    request?: ListShadowMCPApprovalRequestsRequest | undefined,
    security?: ListShadowMCPApprovalRequestsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListShadowMCPApprovalRequestsResult>;
  /**
   * listShadowMCPAccessRules access
   *
   * @remarks
   * List managed Shadow MCP allow and deny rules.
   */
  listShadowMCPAccessRules(
    request?: ListShadowMCPAccessRulesRequest | undefined,
    security?: ListShadowMCPAccessRulesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListShadowMCPAccessRulesResult>;
  /**
   * resolveChallenge access
   *
   * @remarks
   * Record resolutions for one or more denied authz challenges. The caller is responsible for assigning the role first.
   */
  resolveChallenge(
    request: ResolveChallengeRequest,
    security?: ResolveChallengeSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ResolveChallengesResult>;
  /**
   * updateMemberRoles access
   *
   * @remarks
   * Update a team member's role assignments.
   */
  updateMemberRoles(
    request: UpdateMemberRolesRequest,
    security?: UpdateMemberRolesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<AccessMember>;
  /**
   * updateRole access
   *
   * @remarks
   * Update an existing custom role.
   */
  updateRole(
    request: UpdateRoleRequest,
    security?: UpdateRoleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Role>;
  /**
   * updateShadowMCPAccessRule access
   *
   * @remarks
   * Update a managed Shadow MCP access rule.
   */
  updateShadowMCPAccessRule(
    request: UpdateShadowMCPAccessRuleRequest,
    security?: UpdateShadowMCPAccessRuleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ShadowMCPAccessRule>;
}
//# sourceMappingURL=access.d.ts.map
