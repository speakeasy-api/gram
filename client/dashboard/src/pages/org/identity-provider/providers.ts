import type { ComponentType } from "react";
import { OktaProviderCard, OktaProviderWorkspace } from "./OktaManagedAuth";

type ProviderIntegration = {
  id: string;
  Card: ComponentType;
  Workspace: ComponentType;
};

/** Each provider owns its status card and workspace; integrations can coexist. */
export const PROVIDERS: readonly ProviderIntegration[] = [
  { id: "okta", Card: OktaProviderCard, Workspace: OktaProviderWorkspace },
];
