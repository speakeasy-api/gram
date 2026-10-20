/**
 * The one-command install for a headless device.
 *
 * The server mints a short-lived download code for a script that carries the
 * bootstrap token, so the raw token never appears in the command, in shell
 * history, or on this page. That endpoint (`createDeviceInstallCommand`) is
 * landing in a sibling change; until it does, this adapter reports that the
 * command is unavailable and the setup panel leads with manual setup instead.
 * Nothing here fakes a request.
 */

export interface DeviceInstallCommand {
  /** Pipes the install script to a shell. */
  command: string;
  /** Saves the script to a file, to read before running it. */
  downloadCommand: string;
  /** When the download code stops working. */
  codeExpiresAt: Date;
  /** The bootstrap token the script carries, to watch for redemption. */
  joinTokenId: string;
  /** When that bootstrap token stops working. */
  tokenExpiresAt: Date;
}

export interface DeviceInstallCommandRequest {
  agentId: string;
  hooksProjectSlug: string;
  expiresInSeconds: number;
}

export type CreateDeviceInstallCommand = (
  request: DeviceInstallCommandRequest,
) => Promise<DeviceInstallCommand>;

/**
 * How to mint an install command, or null while the server cannot.
 *
 * TODO(createDeviceInstallCommand): once the SDK ships the endpoint, return a
 * function that calls it and maps `{command, download_command,
 * code_expires_at, join_token_id, token_expires_at}` onto
 * {@link DeviceInstallCommand}.
 */
export function useCreateDeviceInstallCommand(): CreateDeviceInstallCommand | null {
  return null;
}
