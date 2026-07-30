import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type CreateNotificationMutationVariables = {
  request: operations.CreateNotificationRequest;
  security?: operations.CreateNotificationSecurity | undefined;
  options?: RequestOptions;
};
export type CreateNotificationMutationData = components.Notification;
/**
 * createNotification notifications
 *
 * @remarks
 * Create a notification for the current project
 */
export declare function useCreateNotificationMutation(
  options?: MutationHookOptions<
    CreateNotificationMutationData,
    Error,
    CreateNotificationMutationVariables
  >,
): UseMutationResult<
  CreateNotificationMutationData,
  Error,
  CreateNotificationMutationVariables
>;
export declare function mutationKeyCreateNotification(): MutationKey;
export declare function buildCreateNotificationMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateNotificationMutationVariables,
  ) => Promise<CreateNotificationMutationData>;
};
//# sourceMappingURL=createNotification.d.ts.map
