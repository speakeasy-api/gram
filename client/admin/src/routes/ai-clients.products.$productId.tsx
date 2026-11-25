import { createFileRoute } from "@tanstack/react-router";

import { aiProductNameKey } from "@/lib/adminQueries";
import { AIProductDetail } from "@/pages/ai-clients/AIProductDetail";

export const Route = createFileRoute("/ai-clients/products/$productId")({
  component: AIProductDetail,
  staticData: {
    crumb: ({ productId }) =>
      productId ? { queryKey: aiProductNameKey(productId) } : undefined,
  },
});
