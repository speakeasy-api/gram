import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/ema/")({
  beforeLoad: () => {
    throw redirect({ to: "/ema/apps" });
  },
});
