import { createRootRoute } from "@tanstack/react-router";

import { NotFound } from "@/components/not-found";
import { AdminLayout } from "@/layouts/AdminLayout";

export const Route = createRootRoute({
  component: AdminLayout,
  notFoundComponent: NotFound,
});
