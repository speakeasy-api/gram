import * as z from "zod/v4-mini";
import { GrantEntry, GrantEntry$Outbound } from "./grantentry.js";
/**
 * A batch of permission entries to apply to access-management operations.
 */
export type GrantsForm = {
  /**
   * The permissions to process.
   */
  grants: Array<GrantEntry>;
};
/** @internal */
export type GrantsForm$Outbound = {
  grants: Array<GrantEntry$Outbound>;
};
/** @internal */
export declare const GrantsForm$outboundSchema: z.ZodMiniType<
  GrantsForm$Outbound,
  GrantsForm
>;
export declare function grantsFormToJSON(grantsForm: GrantsForm): string;
//# sourceMappingURL=grantsform.d.ts.map
