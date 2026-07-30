import * as z from "zod/v4-mini";
export type InviteMemberForm = {
  /**
   * Email address to invite
   */
  email: string;
  /**
   * The ID of the organization
   */
  organizationId: string;
};
/** @internal */
export type InviteMemberForm$Outbound = {
  email: string;
  organization_id: string;
};
/** @internal */
export declare const InviteMemberForm$outboundSchema: z.ZodMiniType<
  InviteMemberForm$Outbound,
  InviteMemberForm
>;
export declare function inviteMemberFormToJSON(
  inviteMemberForm: InviteMemberForm,
): string;
//# sourceMappingURL=invitememberform.d.ts.map
