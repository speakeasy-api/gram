import { createFileRoute } from "@tanstack/react-router";

// Outlet-only, for the reason `organizations.tsx` gives.
export const Route = createFileRoute("/projects")({
  staticData: { crumb: "Projects" },
});
