import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type SetIterationModeMutationVariables = {
  request: operations.SetIterationModeRequest;
  security?: operations.SetIterationModeSecurity | undefined;
  options?: RequestOptions;
};
export type SetIterationModeMutationData = components.Toolset;
/**
 * setIterationMode toolsets
 *
 * @remarks
 * Enable or disable iteration mode for a toolset. When enabled, changes to tools are staged as drafts until promoted.
 */
export declare function useSetIterationModeMutation(
  options?: MutationHookOptions<
    SetIterationModeMutationData,
    Error,
    SetIterationModeMutationVariables
  >,
): UseMutationResult<
  SetIterationModeMutationData,
  Error,
  SetIterationModeMutationVariables
>;
export declare function mutationKeySetIterationMode(): MutationKey;
export declare function buildSetIterationModeMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetIterationModeMutationVariables,
  ) => Promise<SetIterationModeMutationData>;
};
//# sourceMappingURL=setIterationMode.d.ts.map
