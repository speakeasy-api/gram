import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateExternalOAuthServerMutationVariables = {
  request: operations.UpdateExternalOAuthServerRequest;
  security?: operations.UpdateExternalOAuthServerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateExternalOAuthServerMutationData = components.Toolset;
/**
 * updateExternalOAuthServer toolsets
 *
 * @remarks
 * Update an external OAuth server's metadata for a toolset
 */
export declare function useUpdateExternalOAuthServerMutation(
  options?: MutationHookOptions<
    UpdateExternalOAuthServerMutationData,
    Error,
    UpdateExternalOAuthServerMutationVariables
  >,
): UseMutationResult<
  UpdateExternalOAuthServerMutationData,
  Error,
  UpdateExternalOAuthServerMutationVariables
>;
export declare function mutationKeyUpdateExternalOAuthServer(): MutationKey;
export declare function buildUpdateExternalOAuthServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateExternalOAuthServerMutationVariables,
  ) => Promise<UpdateExternalOAuthServerMutationData>;
};
//# sourceMappingURL=updateExternalOAuthServer.d.ts.map
