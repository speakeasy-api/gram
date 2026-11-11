import { createFileRoute, Outlet } from "@tanstack/react-router";

// A layout, so the list and a credential page share the crumb.
export const Route = createFileRoute("/platform-credentials")({
  component: Outlet,
  staticData: { crumb: "Platform credentials" },
});
