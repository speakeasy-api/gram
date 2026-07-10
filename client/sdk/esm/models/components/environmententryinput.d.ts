import * as z from "zod/v4-mini";
/**
 * A single environment entry
 */
export type EnvironmentEntryInput = {
  /**
   * The name of the environment variable
   */
  name: string;
  /**
   * The value of the environment variable
   */
  value: string;
};
/** @internal */
export type EnvironmentEntryInput$Outbound = {
  name: string;
  value: string;
};
/** @internal */
export declare const EnvironmentEntryInput$outboundSchema: z.ZodMiniType<
  EnvironmentEntryInput$Outbound,
  EnvironmentEntryInput
>;
export declare function environmentEntryInputToJSON(
  environmentEntryInput: EnvironmentEntryInput,
): string;
//# sourceMappingURL=environmententryinput.d.ts.map
