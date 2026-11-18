import type { JSX } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { DownloadIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  errorMessage,
  gramAdminDownload,
  gramAdminFetch,
} from "@/lib/gramAdminApi";

type Source = { asset_id: string; name: string; filename: string };

export function McpServerSources({
  organizationId,
  projectId,
  mcpServerId,
}: {
  organizationId: string;
  projectId: string;
  mcpServerId: string;
}): JSX.Element {
  const params = new URLSearchParams({
    organization_id: organizationId,
    project_id: projectId,
    mcp_server_id: mcpServerId,
  }).toString();
  const sources = useQuery({
    staleTime: 60_000,
    queryKey: [
      "admin",
      "mcp-server-sources",
      organizationId,
      projectId,
      mcpServerId,
    ],
    queryFn: ({ signal }) =>
      gramAdminFetch<{ sources: Source[] }>(
        `/admin/mcpServerSources?${params}`,
        { signal },
      ),
  });

  return (
    <section
      className="bg-card rounded-lg border"
      aria-labelledby="sources-heading"
    >
      <div className="border-b px-5 py-3.5">
        <h2 id="sources-heading" className="text-[0.9375rem] font-semibold">
          Sources
        </h2>
      </div>
      <div className="px-5 py-4">
        {sources.isRefetchError && (
          <p role="alert" className="text-muted-foreground mb-2 text-sm">
            Unable to refresh sources. Showing the last loaded list.
          </p>
        )}
        {sources.isPending ? (
          <p role="status" className="text-muted-foreground text-sm">
            Loading sources…
          </p>
        ) : sources.isLoadingError ? (
          <div role="alert" className="text-sm">
            <p>Unable to load sources: {errorMessage(sources.error)}</p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void sources.refetch()}
            >
              Retry
            </Button>
          </div>
        ) : sources.data.sources.length === 0 ? (
          <p className="text-muted-foreground text-sm">No OpenAPI sources.</p>
        ) : (
          <ul className="divide-y">
            {sources.data.sources.map((source) => (
              <SourceRow
                key={source.asset_id}
                source={source}
                params={params}
              />
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

function SourceRow({
  source,
  params,
}: {
  source: Source;
  params: string;
}): JSX.Element {
  const download = useMutation({
    mutationFn: async () => {
      const blob = await gramAdminDownload(
        `/admin/mcpServerSourceDownload?${params}&asset_id=${encodeURIComponent(source.asset_id)}`,
      );
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      try {
        link.href = url;
        link.download = source.filename;
        document.body.append(link);
        link.click();
      } finally {
        link.remove();
        URL.revokeObjectURL(url);
      }
    },
  });
  return (
    <li className="py-3 first:pt-0 last:pb-0">
      <div className="flex items-center justify-between gap-3">
        <span className="min-w-0 break-all text-sm">{source.name}</span>
        <Button
          variant="outline"
          size="sm"
          aria-label={`Download ${source.name}`}
          aria-busy={download.isPending}
          disabled={download.isPending}
          onClick={() => download.mutate()}
        >
          <DownloadIcon aria-hidden="true" />
          {download.isPending ? "Downloading…" : "Download"}
        </Button>
      </div>
      {download.isError && (
        <p role="alert" className="text-destructive mt-2 text-sm">
          Unable to download {source.name}: {errorMessage(download.error)}
        </p>
      )}
    </li>
  );
}
