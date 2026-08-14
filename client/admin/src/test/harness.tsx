import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  type AnyRoute,
  type AnyRouter,
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactNode } from "react";

type Options = { initialPath?: string };
type Mounted = Promise<RenderResult & { router: AnyRouter }>;

// A bare root route, not routeTree.gen.ts: a test renders one component, not a
// whole page's route.
export function renderWithApp(ui: ReactNode, options?: Options): Mounted {
  return renderRouteTree(createRootRoute({ component: () => ui }), options);
}

// For a test that needs the real route rather than one component: pass the tree
// from routeTree.gen.ts so `validateSearch` and the route-scoped hooks run.
export async function renderRouteTree(
  routeTree: AnyRoute,
  { initialPath = "/" }: Options = {},
): Mounted {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  // The router resolves its matches asynchronously, so without this the first
  // render paints an empty document and every caller needs findBy*.
  await router.load();

  return {
    router,
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  };
}
