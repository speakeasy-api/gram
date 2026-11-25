import { createFileRoute, Outlet } from "@tanstack/react-router";

import { aiProductsSearch } from "@/pages/ai-clients/aiProductsSearch";

// A layout, so the list and a product page share the crumb and the list's
// filters in the address.
export const Route = createFileRoute("/ai-clients/products")({
  component: Outlet,
  validateSearch: aiProductsSearch,
  staticData: { crumb: "Products" },
});
