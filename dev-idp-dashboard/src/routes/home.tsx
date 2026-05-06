import { createFileRoute } from "@tanstack/react-router";
import { useGramMode } from "@/hooks/use-gram-mode";
import {
  Card,
  CardContent,
  CardHeader,
} from "@/components/ui/card";
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
            <LeftColumnSkeleton />
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

function LeftColumnSkeleton() {
  return (
    <>
      <Card>
        <CardHeader className="border-b pb-4">
          <div className="h-4 w-28 rounded bg-muted animate-pulse" />
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="flex items-center gap-4">
            <div className="size-14 rounded-full bg-muted animate-pulse" />
            <div className="flex-1 space-y-2">
              <div className="h-5 w-40 rounded bg-muted animate-pulse" />
              <div className="h-3 w-32 rounded bg-muted animate-pulse opacity-70" />
              <div className="h-3 w-48 rounded bg-muted animate-pulse opacity-50" />
            </div>
          </div>
          <div className="h-14 rounded-md bg-muted animate-pulse" />
          <div className="border-t pt-4 space-y-2">
            <div className="h-3 w-32 rounded bg-muted animate-pulse opacity-70" />
            <div className="h-9 rounded-md bg-muted animate-pulse" />
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="border-b pb-4">
          <div className="h-4 w-32 rounded bg-muted animate-pulse" />
        </CardHeader>
        <CardContent className="space-y-2">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="h-12 rounded-md bg-muted animate-pulse"
              style={{ opacity: 1 - i * 0.2 }}
            />
          ))}
        </CardContent>
      </Card>
    </>
  );
}

function NoModeFallback() {
  return (
    <Card>
      <CardContent className="py-8 text-sm text-muted-foreground space-y-2">
        <div className="font-medium text-foreground">
          Gram isn't pointed at the dev-idp.
        </div>
        <p>
          Set <code className="font-mono">SPEAKEASY_SERVER_ADDRESS</code>{" "}
          and/or <code className="font-mono">WORKOS_API_URL</code> to a URL
          under <code className="font-mono">$GRAM_DEVIDP_EXTERNAL_URL</code>,
          restart Gram, and refresh this page.
        </p>
      </CardContent>
    </Card>
  );
}
