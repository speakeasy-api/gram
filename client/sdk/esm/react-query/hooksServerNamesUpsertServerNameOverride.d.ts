import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServerNameOverride } from "../models/components/servernameoverride.js";
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
  UpsertServerNameOverrideRequest,
  UpsertServerNameOverrideSecurity,
} from "../models/operations/upsertservernameoverride.js";
import { MutationHookOptions } from "./_types.js";
export type HooksServerNamesUpsertServerNameOverrideMutationVariables = {
  request: UpsertServerNameOverrideRequest;
  security?: UpsertServerNameOverrideSecurity | undefined;
  options?: RequestOptions;
};
export type HooksServerNamesUpsertServerNameOverrideMutationData =
  ServerNameOverride;
export type HooksServerNamesUpsertServerNameOverrideMutationError =
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
 * upsert hooksServerNames
 *
 * @remarks
 * Create or update a server name display override
 */
export declare function useHooksServerNamesUpsertServerNameOverrideMutation(
  options?: MutationHookOptions<
    HooksServerNamesUpsertServerNameOverrideMutationData,
    HooksServerNamesUpsertServerNameOverrideMutationError,
    HooksServerNamesUpsertServerNameOverrideMutationVariables
  >,
): UseMutationResult<
  HooksServerNamesUpsertServerNameOverrideMutationData,
  HooksServerNamesUpsertServerNameOverrideMutationError,
  HooksServerNamesUpsertServerNameOverrideMutationVariables
>;
export declare function mutationKeyHooksServerNamesUpsertServerNameOverride(): MutationKey;
export declare function buildHooksServerNamesUpsertServerNameOverrideMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: HooksServerNamesUpsertServerNameOverrideMutationVariables,
  ) => Promise<HooksServerNamesUpsertServerNameOverrideMutationData>;
};
//# sourceMappingURL=hooksServerNamesUpsertServerNameOverride.d.ts.map
