import { createFileRoute } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "@/components/App";

export const Route = createFileRoute("/")({
  component: Root,
});

function Root() {
  const { queryClient } = Route.useRouteContext();
  return (
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  );
}
