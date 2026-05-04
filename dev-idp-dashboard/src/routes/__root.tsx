/// <reference types="vite/client" />
import {
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
} from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import appCss from "@/styles/app.css?url";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "color-scheme", content: "light dark" },
      { title: "dev-idp dashboard" },
    ],
    links: [{ rel: "stylesheet", href: appCss }],
  }),
  component: RootComponent,
  shellComponent: RootDocument,
});

function RootComponent() {
  const { queryClient } = Route.useRouteContext();
  return (
    <QueryClientProvider client={queryClient}>
      <div className="min-h-screen flex flex-col">
        <header className="border-b border-border px-6 py-4">
          <div className="flex items-center gap-6 max-w-6xl mx-auto">
            <nav className="inline-flex items-center gap-1 rounded-full bg-muted p-1">
              <TopNavLink to="/home">dev-idp</TopNavLink>
              <TopNavLink to="/providers" matchPrefix>
                Providers
              </TopNavLink>
            </nav>
          </div>
        </header>
        <main className="flex-1 px-6 py-6">
          <Outlet />
        </main>
      </div>
    </QueryClientProvider>
  );
}

function TopNavLink({
  to,
  matchPrefix = false,
  children,
}: {
  to: string;
  matchPrefix?: boolean;
  children: ReactNode;
}) {
  return (
    <Link
      to={to}
      activeOptions={{ exact: !matchPrefix }}
      className={cn(
        "px-3 py-1 text-sm rounded-full transition-colors",
        "text-foreground/60 hover:text-foreground",
      )}
      activeProps={{
        className: "bg-background text-foreground hover:text-foreground",
      }}
    >
      {children}
    </Link>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-background text-foreground antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
