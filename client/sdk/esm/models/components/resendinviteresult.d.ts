import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TeamInvite } from "./teaminvite.js";
export type ResendInviteResult = {
  invite: TeamInvite;
};
/** @internal */
export declare const ResendInviteResult$inboundSchema: z.ZodMiniType<
  ResendInviteResult,
  unknown
>;
export declare function resendInviteResultFromJSON(
  jsonString: string,
): SafeParseResult<ResendInviteResult, SDKValidationError>;
//# sourceMappingURL=resendinviteresult.d.ts.map
