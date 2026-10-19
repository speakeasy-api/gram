import type { JSX } from "react";
import { Outlet } from "react-router";
import {
  AccessHubScopeContext,
  useOrganizationAccessHubScope,
  useProjectAccessHubScope,
} from "./accessHubScope";

export function OrganizationAccessHubRoot(): JSX.Element {
  return (
    <AccessHubScopeContext.Provider value={useOrganizationAccessHubScope()}>
      <Outlet />
    </AccessHubScopeContext.Provider>
  );
}

export function ProjectAccessHubRoot(): JSX.Element {
  return (
    <AccessHubScopeContext.Provider value={useProjectAccessHubScope()}>
      <Outlet />
    </AccessHubScopeContext.Provider>
  );
}
