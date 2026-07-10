import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreatePortalSessionResult } from "../models/components/createportalsessionresult.js";
import { GenerateWorkOSAdminPortalLinkResult } from "../models/components/generateworkosadminportallinkresult.js";
import { ListInvitesResult } from "../models/components/listinvitesresult.js";
import { ListUsersResult } from "../models/components/listusersresult.js";
import { OnboardingStatusResult } from "../models/components/onboardingstatusresult.js";
import { Organization } from "../models/components/organization.js";
import { OrganizationInvitation } from "../models/components/organizationinvitation.js";
import { SendEnterpriseAdminOnboardingEmailResult } from "../models/components/sendenterpriseadminonboardingemailresult.js";
import { VerifyOnboardingHooksSetupResult } from "../models/components/verifyonboardinghookssetupresult.js";
import {
  CreatePortalSessionRequest,
  CreatePortalSessionSecurity,
} from "../models/operations/createportalsession.js";
import {
  DisableWebhooksRequest,
  DisableWebhooksSecurity,
} from "../models/operations/disablewebhooks.js";
import {
  EnableWebhooksRequest,
  EnableWebhooksSecurity,
} from "../models/operations/enablewebhooks.js";
import {
  GenerateWorkOSAdminPortalLinkRequest,
  GenerateWorkOSAdminPortalLinkSecurity,
} from "../models/operations/generateworkosadminportallink.js";
import {
  GetOnboardingStatusRequest,
  GetOnboardingStatusSecurity,
} from "../models/operations/getonboardingstatus.js";
import {
  GetOrganizationRequest,
  GetOrganizationSecurity,
} from "../models/operations/getorganization.js";
import {
  ListInvitesRequest,
  ListInvitesSecurity,
} from "../models/operations/listinvites.js";
import {
  ListOrganizationUsersRequest,
  ListOrganizationUsersSecurity,
} from "../models/operations/listorganizationusers.js";
import {
  RemoveOrganizationUserRequest,
  RemoveOrganizationUserSecurity,
} from "../models/operations/removeorganizationuser.js";
import {
  RevokeInviteRequest,
  RevokeInviteSecurity,
} from "../models/operations/revokeinvite.js";
import {
  SendEnterpriseAdminOnboardingEmailRequest,
  SendEnterpriseAdminOnboardingEmailSecurity,
} from "../models/operations/sendenterpriseadminonboardingemail.js";
import {
  SendInviteRequest,
  SendInviteSecurity,
} from "../models/operations/sendinvite.js";
import {
  UpdateInviteRoleRequest,
  UpdateInviteRoleSecurity,
} from "../models/operations/updateinviterole.js";
import {
  VerifyOnboardingHooksSetupRequest,
  VerifyOnboardingHooksSetupSecurity,
} from "../models/operations/verifyonboardinghookssetup.js";
export declare class Organizations extends ClientSDK {
  /**
   * createPortalSession organizations
   *
   * @remarks
   * Create a webhook portal session.
   */
  createPortalSession(
    request?: CreatePortalSessionRequest | undefined,
    security?: CreatePortalSessionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreatePortalSessionResult>;
  /**
   * disableWebhooks organizations
   *
   * @remarks
   * Disable  webhooks for the active organization.
   */
  disableWebhooks(
    request?: DisableWebhooksRequest | undefined,
    security?: DisableWebhooksSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * enableWebhooks organizations
   *
   * @remarks
   * Enable  webhooks for the active organization.
   */
  enableWebhooks(
    request?: EnableWebhooksRequest | undefined,
    security?: EnableWebhooksSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * generateWorkOSAdminPortalLink organizations
   *
   * @remarks
   * Generate a WorkOS Admin Portal link for the given intent (e.g. dsync, sso).
   */
  generateWorkOSAdminPortalLink(
    request: GenerateWorkOSAdminPortalLinkRequest,
    security?: GenerateWorkOSAdminPortalLinkSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GenerateWorkOSAdminPortalLinkResult>;
  /**
   * get organizations
   *
   * @remarks
   * Get the active organization from the session.
   */
  get(
    request?: GetOrganizationRequest | undefined,
    security?: GetOrganizationSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Organization>;
  /**
   * getOnboardingStatus organizations
   *
   * @remarks
   * Get the onboarding status for the active organization by checking WorkOS SSO connections and directory sync state.
   */
  getOnboardingStatus(
    request?: GetOnboardingStatusRequest | undefined,
    security?: GetOnboardingStatusSecurity | undefined,
    options?: RequestOptions,
  ): Promise<OnboardingStatusResult>;
  /**
   * listInvites organizations
   *
   * @remarks
   * List pending WorkOS invitations for the active organization.
   */
  listInvites(
    request?: ListInvitesRequest | undefined,
    security?: ListInvitesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListInvitesResult>;
  /**
   * listUsers organizations
   *
   * @remarks
   * List users in the active organization from Gram organization_user_relationships.
   */
  listUsers(
    request?: ListOrganizationUsersRequest | undefined,
    security?: ListOrganizationUsersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListUsersResult>;
  /**
   * removeUser organizations
   *
   * @remarks
   * Remove a user from the active organization in Gram and delete their WorkOS organization membership.
   */
  removeUser(
    request: RemoveOrganizationUserRequest,
    security?: RemoveOrganizationUserSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * revokeInvite organizations
   *
   * @remarks
   * Revoke a pending WorkOS invitation.
   */
  revokeInvite(
    request: RevokeInviteRequest,
    security?: RevokeInviteSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * sendEnterpriseAdminOnboardingEmail organizations
   *
   * @remarks
   * Send the enterprise admin onboarding email to one or more recipients. The email links each recipient to the wizard for the active organization. Used by the Platform Admin onboarding tools.
   */
  sendEnterpriseAdminOnboardingEmail(
    request: SendEnterpriseAdminOnboardingEmailRequest,
    security?: SendEnterpriseAdminOnboardingEmailSecurity | undefined,
    options?: RequestOptions,
  ): Promise<SendEnterpriseAdminOnboardingEmailResult>;
  /**
   * sendInvite organizations
   *
   * @remarks
   * Send a WorkOS invitation for the active organization.
   */
  sendInvite(
    request: SendInviteRequest,
    security?: SendInviteSecurity | undefined,
    options?: RequestOptions,
  ): Promise<OrganizationInvitation>;
  /**
   * updateInviteRole organizations
   *
   * @remarks
   * Change the role assigned to a pending WorkOS invitation.
   */
  updateInviteRole(
    request: UpdateInviteRoleRequest,
    security?: UpdateInviteRoleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<OrganizationInvitation>;
  /**
   * verifyOnboardingHooksSetup organizations
   *
   * @remarks
   * Return recent hook events for the active organization so the onboarding wizard can confirm that Claude Code, Cursor, or Codex instrumentation is delivering events to Gram. Polled from the confirm-traffic step.
   */
  verifyOnboardingHooksSetup(
    request?: VerifyOnboardingHooksSetupRequest | undefined,
    security?: VerifyOnboardingHooksSetupSecurity | undefined,
    options?: RequestOptions,
  ): Promise<VerifyOnboardingHooksSetupResult>;
}
//# sourceMappingURL=organizations.d.ts.map
