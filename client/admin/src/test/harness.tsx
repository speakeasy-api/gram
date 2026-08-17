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

// `queryClient` is for a test whose subject is what the page does with a record
// it already holds. Seeding one reproduces `useOpenOrganization`, which writes
// the record into the cache before it navigates: without it every mount starts
// on a pending query and the loading state hides the behaviour under test.
type Options = { initialPath?: string; queryClient?: QueryClient };
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
  { initialPath = "/", queryClient }: Options = {},
): Mounted {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  const client =
    queryClient ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

  // The router resolves its matches asynchronously, so without this the first
  // render paints an empty document and every caller needs findBy*.
  await router.load();

  return {
    router,
    ...render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  };
}
