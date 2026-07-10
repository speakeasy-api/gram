import { ElementsConfig } from "#elements/types";

declare const __GRAM_API_URL__: string | undefined;

export function getApiUrl(config: ElementsConfig): string {
  // The api.url in the config should take precedence over the __GRAM_API_URL__ environment variable
  // because it is a user-defined override
  const configuredApiUrl =
    typeof __GRAM_API_URL__ !== "undefined" ? __GRAM_API_URL__ : undefined;
  const apiURL =
    config.api?.url || configuredApiUrl || "https://app.getgram.ai";
  return apiURL.replace(/\/+$/, ""); // Remove trailing slashes
}
