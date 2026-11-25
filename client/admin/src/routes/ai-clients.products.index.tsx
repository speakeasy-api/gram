import { createFileRoute } from "@tanstack/react-router";

import { AIProductList } from "@/pages/ai-clients/AIProductList";

export const Route = createFileRoute("/ai-clients/products/")({
  component: AIProductList,
});
