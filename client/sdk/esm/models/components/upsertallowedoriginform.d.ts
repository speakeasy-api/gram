import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const UpsertAllowedOriginFormStatus: {
  readonly Pending: "pending";
  readonly Approved: "approved";
  readonly Rejected: "rejected";
};
export type UpsertAllowedOriginFormStatus = ClosedEnum<
  typeof UpsertAllowedOriginFormStatus
>;
export type UpsertAllowedOriginForm = {
  /**
   * The origin URL to upsert
   */
  origin: string;
  status?: UpsertAllowedOriginFormStatus | undefined;
};
/** @internal */
export declare const UpsertAllowedOriginFormStatus$outboundSchema: z.ZodMiniEnum<
  typeof UpsertAllowedOriginFormStatus
>;
/** @internal */
export type UpsertAllowedOriginForm$Outbound = {
  origin: string;
  status: string;
};
/** @internal */
export declare const UpsertAllowedOriginForm$outboundSchema: z.ZodMiniType<
  UpsertAllowedOriginForm$Outbound,
  UpsertAllowedOriginForm
>;
export declare function upsertAllowedOriginFormToJSON(
  upsertAllowedOriginForm: UpsertAllowedOriginForm,
): string;
//# sourceMappingURL=upsertallowedoriginform.d.ts.map
