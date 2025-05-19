import { AddButton } from "@/components/add-button";
import { InputDialog } from "@/components/input-dialog";
import { NameAndSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import {
  useCreateToolsetMutation,
  useListToolsetsSuspense,
  useToolsetSuspense,
} from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { CheckCircle2, Copy } from "lucide-react";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { ToolsetEnvironmentBadge } from "./Toolset";
import { useProject } from "@/contexts/Auth";

export function useToolsets() {
  const { data: toolsets, refetch } = useListToolsetsSuspense();
  return Object.assign(toolsets.toolsets, { refetch });
}

export const useToolset = () => {
  const { toolsetSlug } = useParams();

  const { data: toolset, refetch: refetchToolset } = useToolsetSuspense({
    slug: toolsetSlug ?? "",
  });

  return Object.assign(toolset, { refetch: refetchToolset });
};

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  const toolsets = useToolsets();
  const routes = useRoutes();

  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);
  const [toolsetName, setToolsetName] = useState("");
  const createToolsetMutation = useCreateToolsetMutation({
    onSuccess: async (data) => {
      await toolsets.refetch();
      routes.toolsets.toolset.goTo(data.slug);
    },
    onError: (error) => {
      console.error("Failed to create toolset:", error);
    },
  });

  const createToolset = () => {
    createToolsetMutation.mutate({
      request: {
        createToolsetRequestBody: {
          name: toolsetName,
          description: "New Toolset Description",
        },
      },
    });
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton
            onClick={() => setCreateToolsetDialogOpen(true)}
            tooltip="New Toolset"
          />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {toolsets.map((toolset) => (
          <ToolsetCard key={toolset.id} toolset={toolset} />
        ))}
        <CreateThingCard onClick={() => setCreateToolsetDialogOpen(true)}>
          + New Toolset
        </CreateThingCard>
        <InputDialog
          open={createToolsetDialogOpen}
          onOpenChange={setCreateToolsetDialogOpen}
          title="Create a Toolset"
          description="Give your toolset a name."
          inputs={{
            label: "Toolset name",
            placeholder: "Toolset name",
            value: toolsetName,
            onChange: (value) => setToolsetName(value),
            onSubmit: createToolset,
            validate: (value) => value.length > 0,
          }}
        />
      </Page.Body>
    </Page>
  );
}

export function CreateThingCard({
  onClick,
  children,
}: {
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Card
      className="border-dashed border-2 hover:border-muted-foreground/50 bg-transparent cursor-pointer h-36 trans group shadow-none"
      onClick={onClick}
    >
      <Card.Content className="flex items-center justify-center h-full">
        <Heading
          variant="h5"
          className="text-muted-foreground/40 group-hover:text-muted-foreground trans"
        >
          {children}
        </Heading>
      </Card.Content>
    </Card>
  );
}

function ToolsetCard({ toolset }: { toolset: Toolset }) {
  const routes = useRoutes();
  const [mcpModalOpen, setMcpModalOpen] = useState(false);
  const [isCopied, setIsCopied] = useState(false);
  const project = useProject();

  // Example JSON snippet as a string for formatting
  const mcpJson = `{
    "mcpServers": {
      "Gram${toolset.slug.replace(/-/g, "").replace(/^./, c => c.toUpperCase())}": {
        "command": "npx",
        "args": [
          "mcp-remote",
          "${getServerURL()}/mcp/${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug || "default"}",
          "--allow-http",
          "--header",
          "Authorization:\${GRAM_KEY}"
        ],
        "env": {
          "GRAM_KEY": "Bearer <your-key-here>"
        }
      }
    }
  }`;

  const handleCopy = async () => {
    await navigator.clipboard.writeText(mcpJson);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), 2000);
  };

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title>
            <NameAndSlug
              name={toolset.name}
              slug={toolset.slug}
              linkTo={routes.toolsets.toolset.href(toolset.slug)}
            />
          </Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <ToolsetEnvironmentBadge toolset={toolset} />
            <ToolsBadge tools={toolset.httpTools} />
          </Stack>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          <Card.Description className="max-w-2/3">
            {toolset.description}
          </Card.Description>
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(toolset.updatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Content>
        <div className="flex items-center justify-between w-full">
          <div className="flex items-center gap-2">
            <routes.toolsets.toolset.Link params={[toolset.slug]}>
              <Button variant="outline">Edit</Button>
            </routes.toolsets.toolset.Link>
            <routes.playground.Link queryParams={{ toolset: toolset.slug }}>
              <Button
                variant="outline"
                className="group"
                tooltip="Open in chat playground"
              >
                Playground
                <routes.playground.Icon className="text-muted-foreground group-hover:text-foreground trans" />
              </Button>
            </routes.playground.Link>
          </div>
          <div>
            <Button
              variant="outline"
              className="group"
              onClick={() => setMcpModalOpen(true)}
            >
              MCP Server
            </Button>
            <Dialog open={mcpModalOpen} onOpenChange={setMcpModalOpen}>
              <Dialog.Content className="!max-w-3xl !p-10">
                <Dialog.Header>
                  <Dialog.Title>MCP Server</Dialog.Title>
                  <Dialog.Description>
                    Every Gram toolset is represented as a streamable HTTP MCP
                    server. Copy this snippet for an MCP config.
                  </Dialog.Description>
                </Dialog.Header>
                <div className="relative bg-muted p-3 rounded-md my-4">
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleCopy}
                    className="absolute top-3 right-3 z-10 bg-background shadow-md border border-border hover:bg-accent"
                    style={{ boxShadow: "0 2px 8px rgba(0,0,0,0.08)" }}
                  >
                    {isCopied ? (
                      <CheckCircle2 className="h-5 w-5 text-green-500" />
                    ) : (
                      <Copy className="h-5 w-5" />
                    )}
                  </Button>
                  <pre className="break-all whitespace-pre-wrap text-xs max-h-96 overflow-auto pr-10">
                    {mcpJson}
                  </pre>
                </div>
                <div className="flex justify-end">
                  <Button onClick={() => setMcpModalOpen(false)}>Close</Button>
                </div>
              </Dialog.Content>
            </Dialog>
          </div>
        </div>
      </Card.Content>
    </Card>
  );
}
