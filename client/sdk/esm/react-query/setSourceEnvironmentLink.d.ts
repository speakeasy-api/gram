import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SourceEnvironmentLink } from "../models/components/sourceenvironmentlink.js";
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
  SetSourceEnvironmentLinkRequest,
  SetSourceEnvironmentLinkSecurity,
} from "../models/operations/setsourceenvironmentlink.js";
import { MutationHookOptions } from "./_types.js";
export type SetSourceEnvironmentLinkMutationVariables = {
  request: SetSourceEnvironmentLinkRequest;
  security?: SetSourceEnvironmentLinkSecurity | undefined;
  options?: RequestOptions;
};
export type SetSourceEnvironmentLinkMutationData = SourceEnvironmentLink;
export type SetSourceEnvironmentLinkMutationError =
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
 * setSourceEnvironmentLink environments
 *
 * @remarks
 * Set (upsert) a link between a source and an environment
 */
export declare function useSetSourceEnvironmentLinkMutation(
  options?: MutationHookOptions<
    SetSourceEnvironmentLinkMutationData,
    SetSourceEnvironmentLinkMutationError,
    SetSourceEnvironmentLinkMutationVariables
  >,
): UseMutationResult<
  SetSourceEnvironmentLinkMutationData,
  SetSourceEnvironmentLinkMutationError,
  SetSourceEnvironmentLinkMutationVariables
>;
export declare function mutationKeySetSourceEnvironmentLink(): MutationKey;
export declare function buildSetSourceEnvironmentLinkMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetSourceEnvironmentLinkMutationVariables,
  ) => Promise<SetSourceEnvironmentLinkMutationData>;
};
//# sourceMappingURL=setSourceEnvironmentLink.d.ts.map
