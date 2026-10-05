import type { ComponentType } from "react";
import { getOAuthSetupPolicy } from "./oauthPolicies";
import { SlackSetup } from "./SlackSetup";
import type { OAuthSetupGuideProps } from "./types";

const guides = new Map<string, ComponentType<OAuthSetupGuideProps>>([
  ["slack", SlackSetup],
]);

export function getOAuthSetupGuide(
  serverUrl: string | undefined,
): ComponentType<OAuthSetupGuideProps> | undefined {
  const policy = getOAuthSetupPolicy(serverUrl);
  return policy ? guides.get(policy.id) : undefined;
}
