import { createFileRoute } from "@tanstack/react-router";
import { SelfServePage } from "@/pages/SelfServe";

export const Route = createFileRoute("/self-serve")({
  component: SelfServePage,
  staticData: { crumb: "Self-serve signup" },
});
