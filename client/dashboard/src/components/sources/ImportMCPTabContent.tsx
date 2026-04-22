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
import { toast } from "sonner";

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
      className="border-border bg-card hover:border-border-hover flex w-full cursor-pointer items-start gap-3 rounded-lg border p-4 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50"
    >
      <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center rounded-md">
        {server.iconUrl ? (
          <img
            src={server.iconUrl}
            alt={server.title ?? server.registrySpecifier}
            className="h-6 w-6 rounded"
          />
        ) : (
          <ServerIcon className="text-muted-foreground h-5 w-5" />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <h3 className="text-foreground truncate font-medium">
          {server.title ?? server.registrySpecifier}
        </h3>
        <p className="text-muted-foreground mt-1 line-clamp-2 text-sm">
          {server.description}
        </p>
        <p className="text-muted-foreground/70 mt-2 text-xs">
          {server.registrySpecifier} v{server.version}
        </p>
      </div>
      {isImporting && (
        <Loader2Icon className="text-muted-foreground h-4 w-4 animate-spin" />
      )}
    </button>
  );
}

function MCPServerCardSkeleton() {
  return (
    <div className="border-border bg-card flex items-start gap-3 rounded-lg border p-4">
      <Skeleton className="h-10 w-10 shrink-0 rounded-md" />
      <div className="min-w-0 flex-1 space-y-2">
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
          deploymentId: deployment?.id,
          nonBlocking: true,
          upsertExternalMcps: [
            {
              registryId: server.registryId,
              name: server.title ?? server.registrySpecifier,
              slug: generateSlug(server.registrySpecifier),
              registryServerSpecifier: server.registrySpecifier,
            },
          ],
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
        <SearchIcon className="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-neutral-500" />
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
        <div className="border-destructive bg-destructive/10 text-destructive rounded-lg border p-4">
          Failed to load MCP catalog: {error.message}
        </div>
      )}

      {data && data.servers.length === 0 && (
        <div className="text-muted-foreground flex flex-col items-center justify-center py-12">
          <ServerIcon className="mb-4 h-12 w-12" />
          <p>No MCP servers found</p>
          {debouncedSearch && (
            <p className="mt-1 text-sm">Try a different search term</p>
          )}
        </div>
      )}

      {data && data.servers.length > 0 && (
        <div className="grid max-h-[400px] gap-3 overflow-y-auto">
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
