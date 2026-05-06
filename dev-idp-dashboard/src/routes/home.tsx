import { createFileRoute } from "@tanstack/react-router";
import { useGramMode } from "@/hooks/use-gram-mode";
import { LogoutNotice } from "@/components/dashboard/LogoutNotice";
import { CurrentUserCard } from "@/components/dashboard/CurrentUserCard";
import { OrganizationsCard } from "@/components/dashboard/OrganizationsCard";
import { ActiveProviderPanel } from "@/components/dashboard/ActiveProviderPanel";

export const Route = createFileRoute("/home")({
  component: HomePage,
});

function HomePage() {
  const { data, isLoading } = useGramMode();
  const mode = data?.mode ?? null;
  const user = data?.currentUser?.user ?? null;
  const workos = data?.currentUser?.workos ?? null;

  return (
    <div className="max-w-6xl mx-auto space-y-4">
      <LogoutNotice />
      <div className="grid grid-cols-1 lg:grid-cols-[1fr_22rem] gap-4 items-start">
        <div className="space-y-4 min-w-0">
          {isLoading ? (
            <div className="h-48 rounded-md bg-muted animate-pulse" />
          ) : mode ? (
            <>
              <CurrentUserCard mode={mode} user={user} workos={workos} />
              <OrganizationsCard mode={mode} user={user} workos={workos} />
            </>
          ) : (
            <NoModeFallback />
          )}
        </div>
        <ActiveProviderPanel />
      </div>
    </div>
  );
}

function NoModeFallback() {
  return (
    <div className="rounded-md border border-border bg-card p-6 text-sm text-muted-foreground">
      <div className="font-medium text-foreground mb-2">
        Gram isn't pointed at the dev-idp.
      </div>
      Set <code className="font-mono">SPEAKEASY_SERVER_ADDRESS</code> and/or{" "}
      <code className="font-mono">WORKOS_API_URL</code> to a URL under{" "}
      <code className="font-mono">$GRAM_DEVIDP_EXTERNAL_URL</code>, restart
      Gram, and refresh this page.
    </div>
  );
}
