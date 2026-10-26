import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useCreateWorkloadDeviceInstallCommandMutation } from "@gram/client/react-query/createWorkloadDeviceInstallCommand.js";
import { useCallback } from "react";

/**
 * The one-command install for a headless device.
 *
 * The server mints a short-lived, single-use download code for a script that
 * carries a new bootstrap token, so the raw token never appears in the
 * command, in shell history, or on this page.
 */

export interface DeviceInstallCommand {
  /** Downloads successfully to a temporary file before running the script. */
  command: string;
  /** Downloads successfully, keeps the script file, and then runs it. */
  downloadCommand: string;
  /** When the download code stops working. */
  codeExpiresAt: Date;
  /** The bootstrap token the script carries, to watch for redemption. */
  joinTokenId: string;
  /** When that bootstrap token stops working. */
  tokenExpiresAt: Date;
}

interface DeviceInstallCommandRequest {
  agentId: string;
  hooksProjectSlug: string;
  expiresInSeconds: number;
}

export type CreateDeviceInstallCommand = (
  request: DeviceInstallCommandRequest,
) => Promise<DeviceInstallCommand>;

/**
 * The install command is not offered to this organization. The server answers
 * 404 and issues no token, so the page falls back to manual setup rather than
 * showing an error.
 */
export class InstallCommandUnavailable extends Error {
  constructor() {
    super("The one-command install is not available for this organization.");
    this.name = "InstallCommandUnavailable";
  }
}

/**
 * How to mint an install command, or null where it is switched off.
 *
 * The rollout flag decides up front when it has an answer. When it has none
 * (still loading, or not configured, as in local development), the request is
 * tried and the server, which holds the same flag, answers 404 if it is off.
 */
export function useCreateDeviceInstallCommand(): CreateDeviceInstallCommand | null {
  const flag = useFeatureFlag(FEATURE_FLAGS.deviceBootstrapInstall);
  // The panel shows its own failures, and a 404 is not one at all, so the
  // global "Request failed" toast is opted out of.
  const mutation = useCreateWorkloadDeviceInstallCommandMutation({
    onError: () => undefined,
  });
  const { mutateAsync } = mutation;
  const create = useCallback<CreateDeviceInstallCommand>(
    async ({ agentId, hooksProjectSlug, expiresInSeconds }) => {
      try {
        const result = await mutateAsync({
          request: {
            createDeviceInstallCommandRequestBody: {
              agentId,
              hooksProjectSlug,
              expiresInSeconds,
            },
          },
        });
        return {
          command: result.command,
          downloadCommand: result.downloadCommand,
          codeExpiresAt: result.codeExpiresAt,
          joinTokenId: result.joinTokenId,
          tokenExpiresAt: result.tokenExpiresAt,
        };
      } catch (failure) {
        if (
          failure instanceof GramError &&
          failure.statusCode === 404 &&
          failure.message ===
            "device install commands are not available for this organization"
        ) {
          throw new InstallCommandUnavailable();
        }
        throw failure;
      }
    },
    [mutateAsync],
  );
  return flag.status === "disabled" ? null : create;
}
