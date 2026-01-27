import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Server } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query";
import { Button, Input } from "@speakeasy-api/moonshine";
import { useMutation } from "@tanstack/react-query";
import {
  ArrowRight,
  Loader2,
  MessageCircle,
  Plug,
  Plus,
  Settings,
} from "lucide-react";
import { useEffect, useState } from "react";

function generateSlug(name: string): string {
  const lastPart = name.split("/").pop() || name;
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export function useAddServerMutation() {
  const client = useSdkClient();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const mutation = useMutation({
    mutationFn: async ({
      server,
      toolsetName,
    }: {
      server: Server;
      toolsetName: string;
    }) => {
      const slug = generateSlug(server.registrySpecifier);
      const toolUrn = `tools:externalmcp:${slug}:proxy`;

      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          upsertExternalMcps: [
            {
              registryId: server.registryId,
              name: toolsetName,
              slug,
              registryServerSpecifier: server.registrySpecifier,
            },
          ],
        },
      });

      const toolset = await client.toolsets.create({
        createToolsetRequestBody: {
          name: toolsetName,
          description:
            server.description ?? `MCP server: ${server.registrySpecifier}`,
        },
      });

      await client.toolsets.updateBySlug({
        slug: toolset.slug,
        updateToolsetRequestBody: {
          toolUrns: [toolUrn],
          mcpEnabled: true,
          mcpIsPublic: true,
        },
      });

      // Fetch the toolset to get the generated mcpSlug
      const updatedToolset = await client.toolsets.getBySlug({
        slug: toolset.slug,
      });

      return {
        slug: toolset.slug,
        mcpSlug: updatedToolset.mcpSlug,
      };
    },
  });

  return { mutation, refetchDeployment };
}

interface AddServerDialogProps {
  server: Server | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onServerAdded?: () => void;
}

export function AddServerDialog({
  server,
  open,
  onOpenChange,
  onServerAdded,
}: AddServerDialogProps) {
  const routes = useRoutes();
  const [desiredToolsetName, setDesiredToolsetName] = useState("");
  const [createdServer, setCreatedServer] = useState<{
    slug: string;
    mcpSlug: string;
  } | null>(null);
  const { mutation: addServerMutation, refetchDeployment } =
    useAddServerMutation();

  const displayName = server?.title ?? server?.registrySpecifier ?? "";

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) {
      setCreatedServer(null);
      setDesiredToolsetName("");
      addServerMutation.reset();
    }
  }, [open]);

  // Set default name when server changes
  useEffect(() => {
    if (server) {
      setDesiredToolsetName(server.title ?? "");
    }
  }, [server]);

  const handleSuccess = async (result: {
    slug: string;
    mcpSlug: string | undefined;
  }) => {
    await refetchDeployment();
    onServerAdded?.();
    if (result.mcpSlug) {
      setCreatedServer({ slug: result.slug, mcpSlug: result.mcpSlug });
    }
  };

  if (!server) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="gap-2">
        <Dialog.Header>
          <Dialog.Title>Add to Project</Dialog.Title>
          <Dialog.Description>
            {createdServer ? "" : "Add this MCP server to your project."}
          </Dialog.Description>
        </Dialog.Header>
        {createdServer ? (
          <div className="pb-2">
            <Type small muted className="mb-3">
              <span className="font-medium text-foreground">{displayName}</span>{" "}
              has been added to your project.
            </Type>
            <Type className="font-medium mb-3">Next steps</Type>
            <div className="grid grid-cols-2 gap-2">
              <routes.sources.Link className="no-underline hover:no-underline">
                <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
                  <div className="w-8 h-8 rounded-md bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
                    <Plus className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                  </div>
                  <div className="flex-1">
                    <Type className="text-sm font-medium no-underline">
                      Add more sources
                    </Type>
                  </div>
                  <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                </div>
              </routes.sources.Link>
              <routes.elements.Link
                className="no-underline hover:no-underline"
                queryParams={{ toolset: createdServer.slug }}
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
                href={`${getServerURL()}/mcp/${createdServer.mcpSlug}/install`}
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
                params={[createdServer.slug]}
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
        ) : (
          <AddServerForm
            server={server}
            desiredToolsetName={desiredToolsetName}
            setDesiredToolsetName={setDesiredToolsetName}
            addServerMutation={addServerMutation}
            onCancel={() => onOpenChange(false)}
            onSuccess={handleSuccess}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

type AddServerResult = { slug: string; mcpSlug: string | undefined };

function AddServerForm({
  server,
  desiredToolsetName,
  setDesiredToolsetName,
  addServerMutation,
  onCancel,
  onSuccess,
}: {
  server: Server;
  desiredToolsetName: string;
  setDesiredToolsetName: (name: string) => void;
  addServerMutation: ReturnType<
    typeof useMutation<
      AddServerResult,
      Error,
      { server: Server; toolsetName: string }
    >
  >;
  onCancel: () => void;
  onSuccess: (result: AddServerResult) => void;
}) {
  const handleSubmit = () => {
    addServerMutation.mutate(
      {
        server,
        toolsetName:
          desiredToolsetName || server.title || server.registrySpecifier,
      },
      {
        onSuccess,
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
    <div onKeyDown={handleKeyDown}>
      <div className="flex flex-col gap-2 py-2">
        <Label>Source name</Label>
        <Input
          placeholder={server.title || server.registrySpecifier}
          value={desiredToolsetName}
          onChange={(e) => setDesiredToolsetName(e.target.value)}
          disabled={addServerMutation.isPending}
        />
      </div>
      <Dialog.Footer>
        <Button
          variant="secondary"
          onClick={onCancel}
          disabled={addServerMutation.isPending}
        >
          Cancel
        </Button>
        <Button disabled={addServerMutation.isPending} onClick={handleSubmit}>
          {addServerMutation.isPending ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin mr-2" />
              <Button.Text>Adding...</Button.Text>
            </>
          ) : (
            <Button.Text>Add</Button.Text>
          )}
        </Button>
      </Dialog.Footer>
    </div>
  );
}
