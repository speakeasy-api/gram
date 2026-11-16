import { createFileRoute } from "@tanstack/react-router";
import { MemoryEvalsPage } from "@/pages/memory-evals/MemoryEvals";

export const Route = createFileRoute("/memory-evals")({
  component: MemoryEvalsPage,
  staticData: { crumb: "Organizational Memory evals" },
});
