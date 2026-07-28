import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SetPluginAssignmentsResponseBody } from "../models/components/setpluginassignmentsresponsebody.js";
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
  SetPluginAssignmentsRequest,
  SetPluginAssignmentsSecurity,
} from "../models/operations/setpluginassignments.js";
import { MutationHookOptions } from "./_types.js";
export type SetPluginAssignmentsMutationVariables = {
  request: SetPluginAssignmentsRequest;
  security?: SetPluginAssignmentsSecurity | undefined;
  options?: RequestOptions;
};
export type SetPluginAssignmentsMutationData = SetPluginAssignmentsResponseBody;
export type SetPluginAssignmentsMutationError =
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
 * setPluginAssignments plugins
 *
 * @remarks
 * Replace all assignments for a plugin with the given list of principal URNs.
 */
export declare function useSetPluginAssignmentsMutation(
  options?: MutationHookOptions<
    SetPluginAssignmentsMutationData,
    SetPluginAssignmentsMutationError,
    SetPluginAssignmentsMutationVariables
  >,
): UseMutationResult<
  SetPluginAssignmentsMutationData,
  SetPluginAssignmentsMutationError,
  SetPluginAssignmentsMutationVariables
>;
export declare function mutationKeySetPluginAssignments(): MutationKey;
export declare function buildSetPluginAssignmentsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SetPluginAssignmentsMutationVariables,
  ) => Promise<SetPluginAssignmentsMutationData>;
};
//# sourceMappingURL=setPluginAssignments.d.ts.map
