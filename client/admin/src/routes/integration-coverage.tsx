import { createFileRoute } from "@tanstack/react-router";
import { IntegrationCoverage } from "@/pages/coverage/IntegrationCoverage";

export const Route = createFileRoute("/integration-coverage")({
  component: IntegrationCoverage,
  staticData: { crumb: "Support matrix" },
});
