import { createFileRoute, Outlet } from "@tanstack/react-router";
import { registrySearch } from "@/pages/registry/registrySearch";

// A layout, so the list and an entry page share the crumb and the list's
// filters in the address. The crumb takes the filters with it back to the list.
export const Route = createFileRoute("/registry")({
  component: Outlet,
  validateSearch: registrySearch,
  staticData: { crumb: "MCP Registry" },
});
