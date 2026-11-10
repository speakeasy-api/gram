import { createFileRoute } from "@tanstack/react-router";
export const Route = createFileRoute("/users")({
  staticData: { crumb: "Users" },
});
