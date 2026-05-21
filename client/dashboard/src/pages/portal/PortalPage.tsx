import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { usePortal } from "@gram/client/react-query/portal";
import { Stack } from "@speakeasy-api/moonshine";
import { Link, useParams, useSearchParams } from "react-router";
import { PortalCard } from "./PortalCard";
import { PortalHeader } from "./PortalHeader";

export function PortalPage() {
  const { projectSlug = "" } = useParams();
  const [search] = useSearchParams();
  const preview = search.get("preview") === "1";

  const {
    data: portal,
    error,
    isLoading,
  } = usePortal(
    { gramProject: projectSlug, preview: preview || undefined },
    undefined,
    { enabled: !!projectSlug },
  );

  if (isLoading) {
    return <PortalLoading />;
  }

  if (error || !portal) {
    return <PortalNotFound />;
  }

  return (
    <Stack className="mx-auto max-w-5xl p-8" gap={6}>
      <PortalHeader portal={portal} />
      {portal.servers.length === 0 ? (
        <PortalEmptyState projectSlug={projectSlug} />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {portal.servers.map((s) => (
            <PortalCard key={s.slug} server={s} />
          ))}
        </div>
      )}
      <PortalFooter />
    </Stack>
  );
}

function PortalLoading() {
  return (
    <div className="text-muted-foreground p-8">
      <Type muted>Loading…</Type>
    </div>
  );
}

function PortalNotFound() {
  return (
    <div className="p-8">
      <Heading variant="h2">Portal not found</Heading>
      <Type muted className="mt-2">
        This portal does not exist or has not been published.
      </Type>
    </div>
  );
}

function PortalEmptyState({ projectSlug }: { projectSlug: string }) {
  const session = useSession();
  const orgSlug = session.organization.slug;
  const catalogHref =
    orgSlug && projectSlug
      ? `/${orgSlug}/projects/${projectSlug}/catalog`
      : undefined;

  return (
    <div className="rounded-xl border p-8 text-center">
      <Type muted>
        No MCP servers in this project yet.{" "}
        {catalogHref ? (
          <>
            Add servers from the{" "}
            <Link
              to={catalogHref}
              className="hover:text-foreground underline underline-offset-2"
            >
              catalog
            </Link>{" "}
            to populate the portal.
          </>
        ) : (
          "Add servers from the catalog to populate the portal."
        )}
      </Type>
    </div>
  );
}

function PortalFooter() {
  return (
    <footer className="pt-8">
      <Type small muted>
        Powered by Gram
      </Type>
    </footer>
  );
}
