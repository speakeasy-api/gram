import { getServerURL } from "@/lib/utils";

const PUBLIC_MARKETPLACE_REPO_URL =
  "https://github.com/speakeasy-api/marketplace";
// Keep this token in lockstep with localPlatformMCPMarketplaceToken in
// server/cmd/gram/start.go. Standard local startup serves the generated
// first-party marketplace from this Git Smart HTTP URL.
const LOCAL_MARKETPLACE_REPO_PATH =
  "/marketplace/local-platform-mcp-marketplace-000000000000.git";

export function platformMCPMarketplaceRepoURL(
  isDevelopment = import.meta.env.DEV,
  serverURL?: string,
): string {
  if (!isDevelopment) return PUBLIC_MARKETPLACE_REPO_URL;
  const resolvedServerURL = serverURL ?? getServerURL();
  return `${resolvedServerURL.replace(/\/$/, "")}${LOCAL_MARKETPLACE_REPO_PATH}`;
}
