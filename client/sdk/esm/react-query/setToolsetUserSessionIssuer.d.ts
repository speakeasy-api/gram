import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
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
  SetToolsetUserSessionIssuerRequest,
  SetToolsetUserSessionIssuerSecurity,
} from "../models/operations/settoolsetusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type SetToolsetUserSessionIssuerMutationVariables = {
  request: SetToolsetUserSessionIssuerRequest;
  security?: SetToolsetUserSessionIssuerSecurity | undefined;
  options?: RequestOptions;
};
export type SetToolsetUserSessionIssuerMutationData = Toolset;
export type SetToolsetUserSessionIssuerMutationError =
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
 * setUserSessionIssuer toolsets
 *
 * @remarks
 * Link a toolset to a user_session_issuer (or pass null to unlink). The user_session_issuer must already exist in the caller's project.
 */
export declare function useSetToolsetUserSessionIssuerMutation(
  options?: MutationHookOptions<
    SetToolsetUserSessionIssuerMutationData,
    SetToolsetUserSessionIssuerMutationError,
    SetToolsetUserSessionIssuerMutationVariables
  >,
): UseMutationResult<
  SetToolsetUserSessionIssuerMutationData,
  SetToolsetUserSessionIssuerMutationError,
  SetToolsetUserSessionIssuerMutationVariables
>;
export declare function mutationKeySetToolsetUserSessionIssuer(): MutationKey;
export declare function buildSetToolsetUserSessionIssuerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetToolsetUserSessionIssuerMutationVariables,
  ) => Promise<SetToolsetUserSessionIssuerMutationData>;
};
//# sourceMappingURL=setToolsetUserSessionIssuer.d.ts.map
