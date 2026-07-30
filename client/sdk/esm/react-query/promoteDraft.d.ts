import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type PromoteDraftMutationVariables = {
  request: operations.PromoteDraftRequest;
  security?: operations.PromoteDraftSecurity | undefined;
  options?: RequestOptions;
};
export type PromoteDraftMutationData = components.Toolset;
/**
 * promoteDraft toolsets
 *
 * @remarks
 * Promote the draft toolset changes to production. This copies draft tool URNs and variations to the live version.
 */
export declare function usePromoteDraftMutation(
  options?: MutationHookOptions<
    PromoteDraftMutationData,
    Error,
    PromoteDraftMutationVariables
  >,
): UseMutationResult<
  PromoteDraftMutationData,
  Error,
  PromoteDraftMutationVariables
>;
export declare function mutationKeyPromoteDraft(): MutationKey;
export declare function buildPromoteDraftMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: PromoteDraftMutationVariables,
  ) => Promise<PromoteDraftMutationData>;
};
//# sourceMappingURL=promoteDraft.d.ts.map
