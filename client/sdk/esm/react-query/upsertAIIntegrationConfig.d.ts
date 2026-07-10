import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AIIntegrationConfig } from "../models/components/aiintegrationconfig.js";
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
  UpsertAIIntegrationConfigRequest,
  UpsertAIIntegrationConfigSecurity,
} from "../models/operations/upsertaiintegrationconfig.js";
import { MutationHookOptions } from "./_types.js";
export type UpsertAIIntegrationConfigMutationVariables = {
  request: UpsertAIIntegrationConfigRequest;
  security?: UpsertAIIntegrationConfigSecurity | undefined;
  options?: RequestOptions;
};
export type UpsertAIIntegrationConfigMutationData = AIIntegrationConfig;
export type UpsertAIIntegrationConfigMutationError =
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
 * upsertConfig aiIntegrations
 *
 * @remarks
 * Create or update the org-wide AI integration config for a provider.
 */
export declare function useUpsertAIIntegrationConfigMutation(
  options?: MutationHookOptions<
    UpsertAIIntegrationConfigMutationData,
    UpsertAIIntegrationConfigMutationError,
    UpsertAIIntegrationConfigMutationVariables
  >,
): UseMutationResult<
  UpsertAIIntegrationConfigMutationData,
  UpsertAIIntegrationConfigMutationError,
  UpsertAIIntegrationConfigMutationVariables
>;
export declare function mutationKeyUpsertAIIntegrationConfig(): MutationKey;
export declare function buildUpsertAIIntegrationConfigMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpsertAIIntegrationConfigMutationVariables,
  ) => Promise<UpsertAIIntegrationConfigMutationData>;
};
//# sourceMappingURL=upsertAIIntegrationConfig.d.ts.map
