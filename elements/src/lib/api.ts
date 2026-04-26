import { ElementsConfig, ServerUrl } from "@/types";

export function getApiUrl(config: ElementsConfig): string {
  // The api.url in the config should take precedence over the __GRAM_API_URL__ environment variable
  // because it is a user-defined override
  const apiURL =
    config.api?.url || __GRAM_API_URL__ || "https://app.getgram.ai";
  return apiURL.replace(/\/+$/, ""); // Remove trailing slashes
}

/**
 * Normalizes `config.mcp` (which may be a single URL, an array of URLs, or
 * undefined) into an array of URLs. Empty strings and duplicates are
 * preserved so callers can see exactly what was configured — ordering
 * matters when resolving tool-name collisions.
 */
export function asMcpUrls(mcp: ServerUrl | ServerUrl[] | undefined): string[] {
  if (mcp === undefined) return [];
  if (typeof mcp === "string") return mcp === "" ? [] : [mcp];
  return mcp.filter((u) => typeof u === "string" && u !== "");
}
