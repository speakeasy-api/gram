import type { ComponentType } from "react";
import { OktaProviderCard, OktaWorkspace } from "./OktaWorkspace";
import type { ProviderId } from "./tabs";

type ProviderIntegration = {
  id: ProviderId;
  Card: ComponentType;
  Workspace: ComponentType;
};

/** Each provider owns its status card and workspace; integrations can coexist. */
export const PROVIDERS: readonly ProviderIntegration[] = [
  { id: "okta", Card: OktaProviderCard, Workspace: OktaWorkspace },
];
