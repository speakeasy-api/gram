import * as z from "zod/v4-mini";
export type CreateKeyForm = {
  /**
   * The name of the key
   */
  name: string;
  /**
   * The scopes of the key that determines its permissions.
   */
  scopes: Array<string>;
};
/** @internal */
export type CreateKeyForm$Outbound = {
  name: string;
  scopes: Array<string>;
};
/** @internal */
export declare const CreateKeyForm$outboundSchema: z.ZodMiniType<
  CreateKeyForm$Outbound,
  CreateKeyForm
>;
export declare function createKeyFormToJSON(
  createKeyForm: CreateKeyForm,
): string;
//# sourceMappingURL=createkeyform.d.ts.map
