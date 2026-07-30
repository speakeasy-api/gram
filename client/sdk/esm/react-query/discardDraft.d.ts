import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type DiscardDraftMutationVariables = {
  request: operations.DiscardDraftRequest;
  security?: operations.DiscardDraftSecurity | undefined;
  options?: RequestOptions;
};
export type DiscardDraftMutationData = components.Toolset;
/**
 * discardDraft toolsets
 *
 * @remarks
 * Discard any pending draft changes for a toolset.
 */
export declare function useDiscardDraftMutation(
  options?: MutationHookOptions<
    DiscardDraftMutationData,
    Error,
    DiscardDraftMutationVariables
  >,
): UseMutationResult<
  DiscardDraftMutationData,
  Error,
  DiscardDraftMutationVariables
>;
export declare function mutationKeyDiscardDraft(): MutationKey;
export declare function buildDiscardDraftMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DiscardDraftMutationVariables,
  ) => Promise<DiscardDraftMutationData>;
};
//# sourceMappingURL=discardDraft.d.ts.map
