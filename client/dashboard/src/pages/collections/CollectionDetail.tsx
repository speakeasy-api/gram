import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { Badge } from "@/components/ui/badge";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
} from "@speakeasy-api/moonshine";
import {
  Calendar,
  ChevronDown,
  Download,
  Eye,
  Lock,
  MessageSquare,
  Server,
  Sparkles,
  Wrench,
} from "lucide-react";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { useCollectionDetail } from "./hooks";
import { InstallCollectionDialog } from "./InstallCollectionDialog";

export function CollectionDetailRoot() {
  return <Outlet />;
}

export default function CollectionDetail() {
  const { collectionId } = useParams<{ collectionId: string }>();
  const { data: collection, isLoading } = useCollectionDetail(
    collectionId ?? "",
  );
  const [showInstallDialog, setShowInstallDialog] = useState(false);

  if (isLoading) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Skeleton className="h-[400px]" />
        </Page.Body>
      </Page>
    );
  }

  if (!collection) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Card>
            <Card.Content className="py-12 text-center">
              <Server className="w-12 h-12 mx-auto mb-4 text-muted-foreground" />
              <Type variant="subheading">Collection not found</Type>
              <Type small muted className="mt-2">
                This collection may have been removed or is no longer available.
              </Type>
            </Card.Content>
          </Card>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [collectionId ?? ""]: collection.name }}
        />
      </Page.Header>
      <Page.Body>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          {/* Left column — collection info */}
          <div className="lg:col-span-2 space-y-6">
            {/* Header */}
            <div className="flex items-start gap-6">
              <div className="w-16 h-16 rounded-xl bg-primary/5 flex items-center justify-center shrink-0">
                <Server className="w-8 h-8 text-muted-foreground" />
              </div>
              <div className="flex-1 min-w-0">
                <Stack
                  direction="horizontal"
                  gap={2}
                  align="center"
                  className="mb-1"
                >
                  <h1 className="text-2xl font-bold truncate">
                    {collection.name}
                  </h1>
                  {collection.visibility === "private" ? (
                    <Badge variant="outline">
                      <Lock className="w-3 h-3 mr-1" />
                      Private
                    </Badge>
                  ) : (
                    <Badge variant="secondary">
                      <Eye className="w-3 h-3 mr-1" />
                      Public
                    </Badge>
                  )}
                </Stack>
                <Type small muted>
                  by {collection.author.orgName}
                </Type>
                <div className="mt-4 relative inline-flex rounded-md shadow-sm">
                  <Button
                    className="rounded-r-none"
                    onClick={() => setShowInstallDialog(true)}
                  >
                    <Button.Text>Install</Button.Text>
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        className="inline-flex items-center rounded-r-md bg-primary px-2 border-l border-l-primary-foreground/30 hover:bg-primary/90 transition-colors"
                      >
                        <ChevronDown className="w-4 h-4 text-primary-foreground" />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        onClick={() => setShowInstallDialog(true)}
                      >
                        <Server className="w-4 h-4 mr-2" />
                        Install servers
                      </DropdownMenuItem>
                      <DropdownMenuItem>
                        <Sparkles className="w-4 h-4 mr-2" />
                        Install Claude Plugin
                      </DropdownMenuItem>
                      <DropdownMenuItem>
                        <MessageSquare className="w-4 h-4 mr-2" />
                        Install Slack App
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            </div>

            {/* About */}
            <Card>
              <Card.Header>
                <Card.Title>About</Card.Title>
              </Card.Header>
              <Card.Content>
                <Type className="whitespace-pre-wrap">
                  {collection.description}
                </Type>
              </Card.Content>
            </Card>

            {/* Servers */}
            <Card>
              <Card.Header>
                <Card.Title>
                  MCP Servers ({collection.servers.length})
                </Card.Title>
              </Card.Header>
              <Card.Content>
                <Stack gap={3}>
                  {collection.servers.map((server) => (
                    <div
                      key={server.registrySpecifier}
                      className="flex items-start gap-4 p-4 rounded-lg bg-muted/50"
                    >
                      <div className="w-10 h-10 rounded-lg bg-background flex items-center justify-center shrink-0 border">
                        {server.iconUrl ? (
                          <img
                            src={server.iconUrl}
                            alt={server.title}
                            className="w-6 h-6 object-contain"
                          />
                        ) : (
                          <Server className="w-5 h-5 text-muted-foreground" />
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <Stack
                          direction="horizontal"
                          gap={2}
                          align="center"
                          className="mb-1"
                        >
                          <Type
                            variant="subheading"
                            as="div"
                            className="text-sm"
                          >
                            {server.title}
                          </Type>
                          <Badge variant="secondary" className="shrink-0">
                            <Wrench className="w-3 h-3 mr-1" />
                            {server.toolCount} tools
                          </Badge>
                        </Stack>
                        <Type small muted>
                          {server.description}
                        </Type>
                        <Type
                          small
                          muted
                          className="mt-1 font-mono text-xs opacity-60"
                        >
                          {server.registrySpecifier}
                        </Type>
                      </div>
                    </div>
                  ))}
                </Stack>
              </Card.Content>
            </Card>

            {/* Config placeholder */}
            <Card>
              <Card.Header>
                <Card.Title>Configuration</Card.Title>
              </Card.Header>
              <Card.Content>
                <div className="flex items-center gap-3 p-4 rounded-lg border border-dashed text-muted-foreground">
                  <Wrench className="w-5 h-5 shrink-0" />
                  <Type small muted>
                    Configuration options coming soon. Collections will support
                    custom environment variables and server-level settings.
                  </Type>
                </div>
              </Card.Content>
            </Card>
          </div>

          {/* Right column — metadata */}
          <div className="space-y-4">
            <Card>
              <Card.Header>
                <Card.Title>Stats</Card.Title>
              </Card.Header>
              <Card.Content>
                <div className="space-y-3">
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Installs
                    </Type>
                    <Stack direction="horizontal" gap={1} align="center">
                      <Download className="w-3.5 h-3.5 text-muted-foreground" />
                      <Type className="font-medium">
                        {collection.installCount.toLocaleString()}
                      </Type>
                    </Stack>
                  </div>
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Servers
                    </Type>
                    <Type className="font-medium">
                      {collection.servers.length}
                    </Type>
                  </div>
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Total Tools
                    </Type>
                    <Type className="font-medium">
                      {collection.servers.reduce(
                        (sum, s) => sum + s.toolCount,
                        0,
                      )}
                    </Type>
                  </div>
                </div>
              </Card.Content>
            </Card>

            <Card>
              <Card.Header>
                <Card.Title>Details</Card.Title>
              </Card.Header>
              <Card.Content>
                <div className="space-y-3">
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Author
                    </Type>
                    <Type className="font-medium">
                      {collection.author.orgName}
                    </Type>
                  </div>
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Created
                    </Type>
                    <Stack direction="horizontal" gap={1} align="center">
                      <Calendar className="w-3.5 h-3.5 text-muted-foreground" />
                      <Type className="font-medium text-sm">
                        {new Date(collection.createdAt).toLocaleDateString()}
                      </Type>
                    </Stack>
                  </div>
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Updated
                    </Type>
                    <Stack direction="horizontal" gap={1} align="center">
                      <Calendar className="w-3.5 h-3.5 text-muted-foreground" />
                      <Type className="font-medium text-sm">
                        {new Date(collection.updatedAt).toLocaleDateString()}
                      </Type>
                    </Stack>
                  </div>
                  {collection.visibility === "private" &&
                    collection.sharedWithOrgIds &&
                    collection.sharedWithOrgIds.length > 0 && (
                      <div className="flex justify-between gap-4">
                        <Type small muted>
                          Shared with
                        </Type>
                        <Type className="font-medium">
                          {collection.sharedWithOrgIds.length} org
                          {collection.sharedWithOrgIds.length === 1 ? "" : "s"}
                        </Type>
                      </div>
                    )}
                </div>
              </Card.Content>
            </Card>
          </div>
        </div>

        {collection && (
          <InstallCollectionDialog
            collection={collection}
            open={showInstallDialog}
            onOpenChange={setShowInstallDialog}
          />
        )}
      </Page.Body>
    </Page>
  );
}
