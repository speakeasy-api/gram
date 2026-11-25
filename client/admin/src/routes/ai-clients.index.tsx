import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/ai-clients/")({
  beforeLoad: () => {
    throw redirect({ to: "/ai-clients/products", replace: true });
  },
});
