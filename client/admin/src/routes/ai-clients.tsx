import { createFileRoute, Outlet } from "@tanstack/react-router";

// The section layout. Products is its only view for now; a review queue joins
// it later.
export const Route = createFileRoute("/ai-clients")({
  component: Outlet,
  staticData: { crumb: "AI clients" },
});
