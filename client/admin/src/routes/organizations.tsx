import { createFileRoute } from "@tanstack/react-router";

// No component, so the router renders an Outlet. The route exists to give the
// list and every record one parent the breadcrumb can name.
export const Route = createFileRoute("/organizations")({
  staticData: { crumb: "Organizations" },
});
