import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  SwitchAuthScopesRequest,
  SwitchAuthScopesResponse,
  SwitchAuthScopesSecurity,
} from "../models/operations/switchauthscopes.js";
import { MutationHookOptions } from "./_types.js";
export type SwitchScopesMutationVariables = {
  request?: SwitchAuthScopesRequest | undefined;
  security?: SwitchAuthScopesSecurity | undefined;
  options?: RequestOptions;
};
export type SwitchScopesMutationData = SwitchAuthScopesResponse | undefined;
export type SwitchScopesMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * switchScopes auth
 *
 * @remarks
 * Switches the authentication scope to a different organization.
 */
export declare function useSwitchScopesMutation(
  options?: MutationHookOptions<
    SwitchScopesMutationData,
    SwitchScopesMutationError,
    SwitchScopesMutationVariables
  >,
): UseMutationResult<
  SwitchScopesMutationData,
  SwitchScopesMutationError,
  SwitchScopesMutationVariables
>;
export declare function mutationKeySwitchScopes(): MutationKey;
export declare function buildSwitchScopesMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SwitchScopesMutationVariables,
  ) => Promise<SwitchScopesMutationData>;
};
//# sourceMappingURL=switchScopes.d.ts.map
