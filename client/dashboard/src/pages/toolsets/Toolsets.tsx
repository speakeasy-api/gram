import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { SanboxIcon } from "@/routes";
import {
  useCreateToolsetMutation,
  useListToolsetsSuspense,
  useToolsetSuspense,
} from "@gram/sdk/react-query";
import { Toolset } from "@gram/sdk/models/components/toolset";
import { Stack } from "@speakeasy-api/moonshine";
import { Link, Outlet, useNavigate, useParams } from "react-router-dom";
import { useProject } from "@/contexts/Auth";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogContent,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useState } from "react";
import { PlusIcon } from "lucide-react";

export function useToolsets() {
  const project = useProject();
  const { data: toolsets, refetch } = useListToolsetsSuspense({
    gramProject: project.projectSlug,
  });
  return Object.assign(toolsets.toolsets, { refetch });
}

export const useToolset = () => {
  const { toolsetSlug } = useParams();

  const project = useProject();

  const { data: toolset, refetch: refetchToolset } = useToolsetSuspense({
    gramProject: project.projectSlug,
    slug: toolsetSlug ?? "",
  });

  return Object.assign(toolset, { refetch: refetchToolset });
};

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  const project = useProject();
  const navigate = useNavigate();
  const toolsets = useToolsets();

  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);
  const [toolsetName, setToolsetName] = useState("");
  const createToolsetMutation = useCreateToolsetMutation({
    onSuccess: (data) => {
      toolsets.refetch();
      navigate(`/toolsets/${data.slug}`);
    },
    onError: (error) => {
      console.error("Failed to create toolset:", error);
    },
  });

  const createToolset = () => {
    createToolsetMutation.mutate({
      request: {
        gramProject: project.projectSlug,
        createToolsetRequestBody: {
          name: toolsetName,
          description: "New Toolset Description",
        },
      },
    });
  };

  const addButton = (
    <Button
      variant="ghost"
      className="text-muted-foreground hover:text-foreground"
      onClick={() => {
        setCreateToolsetDialogOpen(true);
      }}
      tooltip="New Toolset"
    >
      <PlusIcon className="w-4 h-4" />
    </Button>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{addButton}</Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {toolsets.map((toolset) => (
          <ToolsetCard key={toolset.id} toolset={toolset} />
        ))}
        <CreateThingCard onClick={() => setCreateToolsetDialogOpen(true)}>
          + New Toolset
        </CreateThingCard>
        <Dialog
          open={createToolsetDialogOpen}
          onOpenChange={setCreateToolsetDialogOpen}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create a Toolset</DialogTitle>
              <DialogDescription>Give your toolset a name.</DialogDescription>
            </DialogHeader>
            <Input
              placeholder="Toolset name"
              value={toolsetName}
              onChange={(e) => setToolsetName(e.target.value)}
              onEnter={createToolset}
            />
            <DialogFooter>
              <Button
                variant="ghost"
                onClick={() => setCreateToolsetDialogOpen(false)}
              >
                Back
              </Button>
              <Button
                onClick={createToolset}
                disabled={toolsetName.length === 0}
              >
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
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
  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Link to={`/toolsets/${toolset.slug}`} className="hover:underline">
            <Card.Title>{toolset.name}</Card.Title>
          </Link>
          <div className="flex gap-2 items-center">
            {toolset.defaultEnvironmentSlug && (
              <Link to={`/environments/${toolset.defaultEnvironmentSlug}`}>
                <Badge className="h-6 flex items-center">Default Env</Badge>
              </Link>
            )}
            <Badge className="h-6 flex items-center">{toolset.httpToolNames?.length || "No"} Tools</Badge>
          </div>
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
        <div className="flex items-center gap-2">
          <Link to={`/toolsets/${toolset.slug}`}>
            <Button variant="outline">Edit</Button>
          </Link>
          <Link to={`/sandbox?toolset=${toolset.slug}`}>
            <Button
              variant="outline"
              className="group"
              tooltip="Open in chat sandbox"
            >
              Sandbox
              <SanboxIcon className="text-muted-foreground group-hover:text-foreground trans" />
            </Button>
          </Link>
        </div>
      </Card.Content>
    </Card>
  );
}
