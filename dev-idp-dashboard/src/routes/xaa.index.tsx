import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/xaa/")({
  beforeLoad: () => {
    throw redirect({ to: "/xaa/apps" });
  },
});
