import { Skeleton } from "@/components/ui/skeleton";
import {
  useLatestDeployment,
  useListMCPCatalog,
  useEvolveDeploymentMutation,
} from "@gram/client/react-query";
import type { ExternalMCPServer } from "@gram/client/models/components";
import { Input } from "@speakeasy-api/moonshine";
import { SearchIcon, ServerIcon, Loader2Icon } from "lucide-react";
import React from "react";
import { toast } from "@/lib/toast";

function generateSlug(name: string): string {
  // Extract the last part after "/" for reverse-DNS names like "ai.exa/exa"
  const lastPart = name.split("/").pop() || name;
  // Convert to lowercase, replace non-alphanumeric with dashes, collapse multiple dashes
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

function MCPServerCard({
  server,
  onImport,
  isImporting,
}: {
  server: ExternalMCPServer;
  onImport: (server: ExternalMCPServer) => void;
  isImporting: boolean;
}) {
  return (
    <button
      type="button"
      onClick={() => onImport(server)}
      disabled={isImporting}
      className="flex items-start gap-3 rounded-lg border border-border bg-card p-4 hover:border-border-hover transition-colors cursor-pointer text-left w-full disabled:opacity-50 disabled:cursor-not-allowed"
    >
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-muted">
        {server.iconUrl ? (
          <img
            src={server.iconUrl}
            alt={server.title ?? server.registrySpecifier}
            className="h-6 w-6 rounded"
          />
        ) : (
          <ServerIcon className="h-5 w-5 text-muted-foreground" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <h3 className="font-medium text-foreground truncate">
          {server.title ?? server.registrySpecifier}
        </h3>
        <p className="text-sm text-muted-foreground line-clamp-2 mt-1">
          {server.description}
        </p>
        <p className="text-xs text-muted-foreground/70 mt-2">
          {server.registrySpecifier} v{server.version}
        </p>
      </div>
      {isImporting && (
        <Loader2Icon className="h-4 w-4 animate-spin text-muted-foreground" />
      )}
    </button>
  );
}

function MCPServerCardSkeleton() {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border bg-card p-4">
      <Skeleton className="h-10 w-10 shrink-0 rounded-md" />
      <div className="flex-1 min-w-0 space-y-2">
        <Skeleton className="h-4 w-[150px]" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-3 w-[100px]" />
      </div>
    </div>
  );
}

interface ImportMCPTabContentProps {
  onSuccess?: () => void;
}

export default function ImportMCPTabContent({
  onSuccess,
}: ImportMCPTabContentProps) {
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const [search, setSearch] = React.useState("");
  const [debouncedSearch, setDebouncedSearch] = React.useState("");
  const [importingServer, setImportingServer] = React.useState<string | null>(
    null,
  );

  const evolveMutation = useEvolveDeploymentMutation();

  React.useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading, error } = useListMCPCatalog(
    debouncedSearch ? { search: debouncedSearch } : undefined,
  );

  const handleImport = async (server: ExternalMCPServer) => {
    const serverKey = `${server.registryId}-${server.registrySpecifier}`;
    setImportingServer(serverKey);

    try {
      await evolveMutation.mutateAsync({
        request: {
          evolveForm: {
            deploymentId: deployment?.id,
            upsertExternalMcps: [
              {
                registryId: server.registryId,
                name: server.title ?? server.registrySpecifier,
                slug: generateSlug(server.registrySpecifier),
                registryServerSpecifier: server.registrySpecifier,
              },
            ],
          },
        },
      });

      await refetchDeployment();
      toast.success(`Imported ${server.title ?? server.registrySpecifier}`);
      onSuccess?.();
    } catch (err) {
      console.error("Failed to import external MCP:", err);
      toast.error(
        `Failed to import ${server.title ?? server.registrySpecifier}`,
      );
    } finally {
      setImportingServer(null);
    }
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="relative">
        <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-neutral-500" />
        <Input
          type="text"
          placeholder="Search MCP servers..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      {isLoading && (
        <div className="grid gap-3">
          <MCPServerCardSkeleton />
          <MCPServerCardSkeleton />
          <MCPServerCardSkeleton />
        </div>
      )}

      {error && (
        <div className="rounded-lg border border-destructive bg-destructive/10 p-4 text-destructive">
          Failed to load MCP catalog: {error.message}
        </div>
      )}

      {data && data.servers.length === 0 && (
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <ServerIcon className="h-12 w-12 mb-4" />
          <p>No MCP servers found</p>
          {debouncedSearch && (
            <p className="text-sm mt-1">Try a different search term</p>
          )}
        </div>
      )}

      {data && data.servers.length > 0 && (
        <div className="grid gap-3 max-h-[400px] overflow-y-auto">
          {data.servers.map((server) => {
            const serverKey = `${server.registryId}-${server.registrySpecifier}`;
            return (
              <MCPServerCard
                key={serverKey}
                server={server}
                onImport={handleImport}
                isImporting={importingServer === serverKey}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
