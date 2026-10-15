import { ElementsConfig } from "@/elements/types";
import { getServerURL } from "@/lib/utils";

export function getApiUrl(config: ElementsConfig): string {
  // The api.url in the config should take precedence over the __GRAM_API_URL__ environment variable
  // because it is a user-defined override
  const apiURL = config.api?.url || __GRAM_API_URL__ || getServerURL();
  return apiURL.replace(/\/+$/, ""); // Remove trailing slashes
}
