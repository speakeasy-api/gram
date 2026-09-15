import { createFileRoute, redirect } from "@tanstack/react-router";

// Redirect before the render pass, so the root path never paints an empty page.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/organizations", replace: true });
  },
});
