import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import { useAddServerMutation } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import {
  useListMCPCatalog,
} from "@gram/client/react-query";
import type { ExternalMCPServer } from "@gram/client/models/components";
import { Button, Input } from "@speakeasy-api/moonshine";
import {
  ArrowLeft,
  ArrowRight,
  Loader2Icon,
  MessageCircle,
  Plug,
  Plus,
  SearchIcon,
  ServerIcon,
  Settings,
} from "lucide-react";
import React from "react";
import { toast } from "sonner";

function MCPServerCard({
  server,
  onSelect,
}: {
  server: ExternalMCPServer;
  onSelect: (server: ExternalMCPServer) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onSelect(server)}
      className="flex items-start gap-3 rounded-lg border border-border bg-card p-4 hover:border-border-hover transition-colors cursor-pointer text-left w-full"
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

type Step =
  | { kind: "browse" }
  | { kind: "name"; server: ExternalMCPServer }
  | { kind: "success"; server: ExternalMCPServer; slug: string; mcpSlug: string };

export default function ImportMCPTabContent({
  onSuccess,
}: ImportMCPTabContentProps) {
  const [step, setStep] = React.useState<Step>({ kind: "browse" });

  if (step.kind === "browse") {
    return (
      <BrowseStep
        onSelect={(server) => setStep({ kind: "name", server })}
      />
    );
  }

  if (step.kind === "name") {
    return (
      <NameStep
        server={step.server}
        onBack={() => setStep({ kind: "browse" })}
        onSuccess={(result) => {
          if (result.mcpSlug) {
            setStep({
              kind: "success",
              server: step.server,
              slug: result.slug,
              mcpSlug: result.mcpSlug,
            });
          } else {
            toast.success(
              `Added ${step.server.title ?? step.server.registrySpecifier}`,
            );
            onSuccess?.();
          }
        }}
      />
    );
  }

  return (
    <SuccessStep
      server={step.server}
      slug={step.slug}
      mcpSlug={step.mcpSlug}
      onAddMore={() => setStep({ kind: "browse" })}
    />
  );
}

function BrowseStep({
  onSelect,
}: {
  onSelect: (server: ExternalMCPServer) => void;
}) {
  const [search, setSearch] = React.useState("");
  const [debouncedSearch, setDebouncedSearch] = React.useState("");

  React.useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading, error } = useListMCPCatalog(
    debouncedSearch ? { search: debouncedSearch } : undefined,
  );

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
                onSelect={onSelect}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

function NameStep({
  server,
  onBack,
  onSuccess,
}: {
  server: ExternalMCPServer;
  onBack: () => void;
  onSuccess: (result: { slug: string; mcpSlug: string | undefined }) => void;
}) {
  const displayName = server.title ?? server.registrySpecifier;
  const [toolsetName, setToolsetName] = React.useState(server.title ?? "");
  const { mutation: addServerMutation, refetchDeployment } =
    useAddServerMutation();

  const handleSubmit = () => {
    addServerMutation.mutate(
      {
        server,
        toolsetName: toolsetName || displayName,
      },
      {
        onSuccess: async (result) => {
          await refetchDeployment();
          onSuccess(result);
        },
        onError: (err) => {
          console.error("Failed to add MCP server:", err);
          toast.error(`Failed to add ${displayName}`);
        },
      },
    );
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !addServerMutation.isPending) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div className="flex flex-col gap-4 p-4" onKeyDown={handleKeyDown}>
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onBack}
          disabled={addServerMutation.isPending}
          className="flex items-center justify-center rounded-md p-1 hover:bg-muted transition-colors disabled:opacity-50"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-muted">
            {server.iconUrl ? (
              <img
                src={server.iconUrl}
                alt={displayName}
                className="h-6 w-6 rounded"
              />
            ) : (
              <ServerIcon className="h-5 w-5 text-muted-foreground" />
            )}
          </div>
          <div>
            <h3 className="font-medium text-foreground">{displayName}</h3>
            <p className="text-xs text-muted-foreground">
              {server.registrySpecifier}
            </p>
          </div>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <label className="text-sm font-medium">Source name</label>
        <Input
          placeholder={displayName}
          value={toolsetName}
          onChange={(e) => setToolsetName(e.target.value)}
          disabled={addServerMutation.isPending}
        />
      </div>

      <div className="flex justify-end gap-2">
        <Button
          variant="tertiary"
          onClick={onBack}
          disabled={addServerMutation.isPending}
        >
          Back
        </Button>
        <Button
          disabled={addServerMutation.isPending}
          onClick={handleSubmit}
        >
          {addServerMutation.isPending ? (
            <>
              <Loader2Icon className="w-4 h-4 animate-spin mr-2" />
              Adding...
            </>
          ) : (
            "Add"
          )}
        </Button>
      </div>
    </div>
  );
}

function SuccessStep({
  server,
  slug,
  mcpSlug,
  onAddMore,
}: {
  server: ExternalMCPServer;
  slug: string;
  mcpSlug: string;
  onAddMore: () => void;
}) {
  const routes = useRoutes();
  const displayName = server.title ?? server.registrySpecifier;

  return (
    <div className="flex flex-col gap-4 p-4">
      <Type small muted>
        <span className="font-medium text-foreground">{displayName}</span> has
        been added to your project.
      </Type>
      <Type className="font-medium">Next steps</Type>
      <div className="grid grid-cols-2 gap-2">
        <button
          type="button"
          onClick={onAddMore}
          className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all text-left"
        >
          <div className="w-8 h-8 rounded-md bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
            <Plus className="w-4 h-4 text-blue-600 dark:text-blue-400" />
          </div>
          <div className="flex-1">
            <Type className="text-sm font-medium">Add more sources</Type>
          </div>
          <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
        </button>
        <routes.elements.Link
          className="no-underline hover:no-underline"
          queryParams={{ toolset: slug }}
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-violet-500/10 dark:bg-violet-500/20 flex items-center justify-center shrink-0">
              <MessageCircle className="w-4 h-4 text-violet-600 dark:text-violet-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Deploy as chat
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </routes.elements.Link>
        <a
          href={`${getServerURL()}/mcp/${mcpSlug}/install`}
          target="_blank"
          rel="noopener noreferrer"
          className="no-underline hover:no-underline"
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-emerald-500/10 dark:bg-emerald-500/20 flex items-center justify-center shrink-0">
              <Plug className="w-4 h-4 text-emerald-600 dark:text-emerald-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Connect via Claude, Cursor
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </a>
        <routes.mcp.details.Link
          params={[slug]}
          className="no-underline hover:no-underline"
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-orange-500/10 dark:bg-orange-500/20 flex items-center justify-center shrink-0">
              <Settings className="w-4 h-4 text-orange-600 dark:text-orange-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Configure MCP settings
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </routes.mcp.details.Link>
      </div>
    </div>
  );
}
