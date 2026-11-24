import { getRuntimeConfig, type RuntimeConfig } from "@/lib/runtime-config";
import { firstPartyHosts, getServerURL } from "@/lib/utils";

/**
 * The server URL each Speakeasy client uses when its configuration names no
 * host. Each value mirrors the client's compiled-in default and must change
 * with it.
 */
export const CLIENT_DEFAULT_SERVER_URLS = {
  /** The speakeasy CLI (workflow.DefaultBaseURL in cli/internal/workflow). */
  cli: "https://app.getgram.ai",
  /** The speakeasy-hooks binary (relay.DefaultServerURL in hooks/relay). */
  hooks: "https://app.getgram.ai",
  /** The device agent daemon (auth.DefaultControlPlaneURL in device-agent). */
  deviceAgent: "https://app.getgram.ai",
} as const;

export type SpeakeasyClient = keyof typeof CLIENT_DEFAULT_SERVER_URLS;

/**
 * The server URL a configuration generated for client must name, or null
 * when the client's own default already reaches this deployment.
 *
 * A default reaches this deployment when its host is one of the deployment's
 * first-party hosts, which all serve the same product. So configurations
 * generated on a US host stay unchanged, while those generated on another
 * partition, a dev environment or a local stack name the serving host.
 */
export function clientServerURLOverride(
  client: SpeakeasyClient,
  serverURL: string = getServerURL(),
  config: RuntimeConfig = getRuntimeConfig(),
): string | null {
  const server = new URL(serverURL, window.location.origin);
  const defaultHost = new URL(CLIENT_DEFAULT_SERVER_URLS[client]).hostname;
  return firstPartyHosts(server.href, config).includes(defaultHost)
    ? null
    : server.origin;
}
