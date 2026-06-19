import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/providers/")({
  beforeLoad: () => {
    throw redirect({
      to: "/providers/$mode",
      params: { mode: "mock-workos" },
    });
  },
});
