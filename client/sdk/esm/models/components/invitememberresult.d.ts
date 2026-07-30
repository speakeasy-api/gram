import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TeamInvite } from "./teaminvite.js";
export type InviteMemberResult = {
  invite: TeamInvite;
};
/** @internal */
export declare const InviteMemberResult$inboundSchema: z.ZodMiniType<
  InviteMemberResult,
  unknown
>;
export declare function inviteMemberResultFromJSON(
  jsonString: string,
): SafeParseResult<InviteMemberResult, SDKValidationError>;
//# sourceMappingURL=invitememberresult.d.ts.map
