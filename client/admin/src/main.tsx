import React from "react";
import ReactDOM from "react-dom/client";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import "./index.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("missing #root element");
}

// gramAdminFetch redirects to /admin/auth.login on 401, so a retry only burns
// time before that redirect lands.
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

const router = createRouter({ routeTree });

// Registers the route tree with the library types, which is what makes `to`,
// `params` and `search` typed at every call site.
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
