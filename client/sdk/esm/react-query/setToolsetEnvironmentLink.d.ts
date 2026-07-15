import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ToolsetEnvironmentLink } from "../models/components/toolsetenvironmentlink.js";
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
  SetToolsetEnvironmentLinkRequest,
  SetToolsetEnvironmentLinkSecurity,
} from "../models/operations/settoolsetenvironmentlink.js";
import { MutationHookOptions } from "./_types.js";
export type SetToolsetEnvironmentLinkMutationVariables = {
  request: SetToolsetEnvironmentLinkRequest;
  security?: SetToolsetEnvironmentLinkSecurity | undefined;
  options?: RequestOptions;
};
export type SetToolsetEnvironmentLinkMutationData = ToolsetEnvironmentLink;
export type SetToolsetEnvironmentLinkMutationError =
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
 * setToolsetEnvironmentLink environments
 *
 * @remarks
 * Set (upsert) a link between a toolset and an environment
 */
export declare function useSetToolsetEnvironmentLinkMutation(
  options?: MutationHookOptions<
    SetToolsetEnvironmentLinkMutationData,
    SetToolsetEnvironmentLinkMutationError,
    SetToolsetEnvironmentLinkMutationVariables
  >,
): UseMutationResult<
  SetToolsetEnvironmentLinkMutationData,
  SetToolsetEnvironmentLinkMutationError,
  SetToolsetEnvironmentLinkMutationVariables
>;
export declare function mutationKeySetToolsetEnvironmentLink(): MutationKey;
export declare function buildSetToolsetEnvironmentLinkMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetToolsetEnvironmentLinkMutationVariables,
  ) => Promise<SetToolsetEnvironmentLinkMutationData>;
};
//# sourceMappingURL=setToolsetEnvironmentLink.d.ts.map
