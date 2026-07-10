import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { EvolveResult } from "../models/components/evolveresult.js";
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
  EvolveDeploymentRequest,
  EvolveDeploymentSecurity,
} from "../models/operations/evolvedeployment.js";
import { MutationHookOptions } from "./_types.js";
export type EvolveDeploymentMutationVariables = {
  request: EvolveDeploymentRequest;
  security?: EvolveDeploymentSecurity | undefined;
  options?: RequestOptions;
};
export type EvolveDeploymentMutationData = EvolveResult;
export type EvolveDeploymentMutationError =
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
 * evolve deployments
 *
 * @remarks
 * Create a new deployment with additional or updated tool sources.
 */
export declare function useEvolveDeploymentMutation(
  options?: MutationHookOptions<
    EvolveDeploymentMutationData,
    EvolveDeploymentMutationError,
    EvolveDeploymentMutationVariables
  >,
): UseMutationResult<
  EvolveDeploymentMutationData,
  EvolveDeploymentMutationError,
  EvolveDeploymentMutationVariables
>;
export declare function mutationKeyEvolveDeployment(): MutationKey;
export declare function buildEvolveDeploymentMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: EvolveDeploymentMutationVariables,
  ) => Promise<EvolveDeploymentMutationData>;
};
//# sourceMappingURL=evolveDeployment.d.ts.map
