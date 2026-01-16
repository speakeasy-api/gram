import { Page } from "@/components/page-layout";
import {
  ExternalMCPServerCard,
  ExternalMCPServerCardLoading,
} from "@/components/sources/ExternalMCPServerCard";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import {
  useEvolveDeploymentMutation,
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Badge, Button, Input } from "@speakeasy-api/moonshine";
import { Loader2Icon, PencilIcon, XIcon, CheckIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { toast } from "sonner";

export default function ExternalMCPDetails() {
  const { sourceSlug } = useParams<{
    sourceSlug: string;
  }>();
  const routes = useRoutes();

  const { data: deployment, isLoading: isLoadingDeployment, refetch: refetchDeployment } =
    useLatestDeployment();

  // Find the specific external MCP server from the deployment
  const source = useMemo(() => {
    if (!deployment?.deployment) return null;

    return deployment.deployment.externalMcps?.find(
      (mcp) => mcp.slug === sourceSlug,
    );
  }, [deployment, sourceSlug]);

  const { data: toolsets, isLoading: isLoadingToolsets } = useListToolsets();

  // User-Agent editing state
  const [isEditingUserAgent, setIsEditingUserAgent] = useState(false);
  const [userAgentValue, setUserAgentValue] = useState("");
  const evolveMutation = useEvolveDeploymentMutation();

  const handleEditUserAgent = () => {
    setUserAgentValue(source?.userAgent ?? "");
    setIsEditingUserAgent(true);
  };

  const handleCancelEditUserAgent = () => {
    setIsEditingUserAgent(false);
    setUserAgentValue("");
  };

  const handleSaveUserAgent = async () => {
    if (!source || !deployment?.deployment?.id) return;

    try {
      await evolveMutation.mutateAsync({
        request: {
          evolveForm: {
            deploymentId: deployment.deployment.id,
            upsertExternalMcps: [
              {
                registryId: source.registryId,
                name: source.name,
                slug: source.slug,
                registryServerSpecifier: source.registryServerSpecifier,
                userAgent: userAgentValue || undefined,
              },
            ],
          },
        },
      });

      await refetchDeployment();
      setIsEditingUserAgent(false);
      toast.success("User-Agent updated successfully");
    } catch (err) {
      console.error("Failed to update User-Agent:", err);
      toast.error("Failed to update User-Agent");
    }
  };

  // Find the toolset that uses this external MCP source
  const associatedToolset = useMemo(() => {
    if (!toolsets?.toolsets || !source) return undefined;

    return toolsets.toolsets.find((t) =>
      t.toolUrns?.includes(`tools:externalmcp:${source.slug}:proxy`),
    );
  }, [toolsets, source]);

  // If source not found, redirect to home
  if (!isLoadingDeployment && !source) {
    return <Navigate to={routes.home.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={["externalmcp"]}
        />
      </Page.Header>

      <Page.Body>
        {/* Header Section with Title and Actions */}
        <div className="flex items-center justify-between mb-4 h-10">
          <div className="flex items-center gap-2">
            <Heading variant="h2" className="normal-case">
              {source?.name || sourceSlug}
            </Heading>
            <Badge variant="neutral">External MCP</Badge>
          </div>
        </div>

        <div className="space-y-6">
          {/* Source Metadata Card */}
          <div className="rounded-lg border bg-card p-6">
            <Type as="h2" className="text-lg font-semibold mb-4">
              Source Information
            </Type>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Name</Type>
                <Type className="font-medium">{source?.name}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Slug</Type>
                <Type className="font-medium">{source?.slug}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Type</Type>
                <Type className="font-medium">External MCP</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Registry ID
                </Type>
                <Type className="font-medium">{source?.registryId}</Type>
              </div>
              <div className="col-span-2">
                <Type className="text-sm text-muted-foreground mb-1">
                  Server Specifier
                </Type>
                <Type className="font-medium">
                  {source?.registryServerSpecifier}
                </Type>
              </div>
              <div className="col-span-2">
                <Type className="text-sm text-muted-foreground mb-1">
                  User-Agent
                </Type>
                {isEditingUserAgent ? (
                  <div className="flex items-center gap-2">
                    <Input
                      type="text"
                      value={userAgentValue}
                      onChange={(e) => setUserAgentValue(e.target.value)}
                      placeholder="Custom User-Agent header (optional)"
                      className="flex-1"
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleCancelEditUserAgent}
                      disabled={evolveMutation.isPending}
                    >
                      <XIcon className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleSaveUserAgent}
                      disabled={evolveMutation.isPending}
                    >
                      {evolveMutation.isPending ? (
                        <Loader2Icon className="h-4 w-4 animate-spin" />
                      ) : (
                        <CheckIcon className="h-4 w-4" />
                      )}
                    </Button>
                  </div>
                ) : (
                  <div className="flex items-center gap-2">
                    <Type className="font-medium">
                      {source?.userAgent || (
                        <span className="text-muted-foreground italic">Not set</span>
                      )}
                    </Type>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleEditUserAgent}
                      className="h-6 w-6 p-0"
                    >
                      <PencilIcon className="h-3 w-3" />
                    </Button>
                  </div>
                )}
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Deployment
                </Type>
                {deployment?.deployment?.id ? (
                  <routes.deployments.deployment.Link
                    params={[deployment.deployment.id]}
                    className="font-medium hover:underline"
                  >
                    {deployment.deployment.id.slice(0, 8)}
                  </routes.deployments.deployment.Link>
                ) : (
                  <Type className="font-medium">None</Type>
                )}
              </div>
            </div>
          </div>

          {/* MCP Server Relationship Card */}
          {isLoadingToolsets ? (
            <ExternalMCPServerCardLoading />
          ) : associatedToolset !== undefined ? (
            <ExternalMCPServerCard toolset={associatedToolset} />
          ) : null}
        </div>
      </Page.Body>
    </Page>
  );
}
