import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type ArchiveNotificationMutationVariables = {
  request: operations.ArchiveNotificationRequest;
  security?: operations.ArchiveNotificationSecurity | undefined;
  options?: RequestOptions;
};
export type ArchiveNotificationMutationData = components.Notification;
/**
 * archiveNotification notifications
 *
 * @remarks
 * Archive a notification
 */
export declare function useArchiveNotificationMutation(
  options?: MutationHookOptions<
    ArchiveNotificationMutationData,
    Error,
    ArchiveNotificationMutationVariables
  >,
): UseMutationResult<
  ArchiveNotificationMutationData,
  Error,
  ArchiveNotificationMutationVariables
>;
export declare function mutationKeyArchiveNotification(): MutationKey;
export declare function buildArchiveNotificationMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ArchiveNotificationMutationVariables,
  ) => Promise<ArchiveNotificationMutationData>;
};
//# sourceMappingURL=archiveNotification.d.ts.map
