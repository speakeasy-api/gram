import { DetailLayout } from "@/components/layouts/detail-layout";
import MonacoEditorLazy from "@/components/monaco-editor.lazy";
import { Page } from "@/components/page-layout";
import { computeTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import { useFetchSourceContent } from "@/components/sources/useFetchSourceContent";
import { SkeletonCode } from "@/components/ui/skeleton";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useListTools } from "@/hooks/toolTypes";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolUpdate } from "@/hooks/useToolUpdate";
import { invalidateAllListTools } from "@gram/client/react-query/listTools.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { SourceDeploymentsPanel } from "./SourceDeploymentsPanel";
import ExternalMCPDetails from "./external-mcp/ExternalMCPDetails";
import RemoteMCPDetails from "./remote-mcp/RemoteMCPDetails";
import TunneledMCPDetails from "./tunneled-mcp/TunneledMCPDetails";
import { SourceOverviewTab } from "./SourceOverviewTab";
import { SourceToolsTab } from "./SourceToolsTab";
import { SourceMCPServersTab } from "./SourceMCPServersTab";
import { SourceSettingsTab } from "./SourceSettingsTab";

// Map dashboard source kinds to backend deployment_logs.attachment_type values.
// See server/internal/deployments/events/log.go.
function attachmentTypeForSourceKind(sourceKind: string | undefined): string {
  switch (sourceKind) {
    case "function":
      return "functions";
    case "externalmcp":
    case "remotemcp":
    case "tunneledmcp":
      return "external_mcp";
    case undefined:
    default:
      return "openapi";
  }
}

export default function SourceDetails(): JSX.Element {
  const { sourceKind, sourceSlug } = useParams<{
    sourceKind: string;
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const { data: deployment, isLoading: isLoadingDeployment } =
    useLatestDeployment();
  const { data: assetsData } = useListAssets();
  const { data: deploymentsData } = useListDeployments({}, {});

  const allDeployments = useMemo(
    () => deploymentsData?.items ?? [],
    [deploymentsData],
  );
  const activeDeploymentItem = useMemo(
    () => allDeployments.find((d) => d.status === "completed") ?? null,
    [allDeployments],
  );

  const gramClient = useGramContext();
  const telemetryFrom = useMemo(() => {
    const d = new Date();
    d.setDate(d.getDate() - 7);
    return d;
  }, []);
  const telemetryTo = useMemo(() => new Date(), []);

  const [activeTab, setActiveTab] = useState(() => {
    const hash = window.location.hash.replace("#", "");
    return hash || "overview";
  });

  const source = useMemo(() => {
    if (!deployment?.deployment) return null;
    if (sourceKind === "http" || sourceKind === "openapi") {
      return deployment.deployment.openapiv3Assets?.find(
        (asset) => asset.slug === sourceSlug,
      );
    } else if (sourceKind === "function") {
      return deployment.deployment.functionsAssets?.find(
        (func) => func.slug === sourceSlug,
      );
    }
    return null;
  }, [deployment, sourceKind, sourceSlug]);

  const underlyingAsset = useMemo(() => {
    if (!source || !assetsData) return null;
    return assetsData.assets.find((a) => a.id === source.assetId) ?? null;
  }, [source, assetsData]);

  const { data: toolsData } = useListTools(
    { deploymentId: deployment?.deployment?.id },
    undefined,
    { enabled: !!deployment?.deployment?.id },
  );

  const relatedTools = useMemo(() => {
    if (!toolsData?.tools || !source) return [];
    const matched = toolsData.tools.filter((tool) => {
      if (tool.type === "http") return tool.openapiv3DocumentId === source.id;
      if (tool.type === "function") return tool.assetId === source.assetId;
      return false;
    });
    const seen = new Set<string>();
    return matched.filter((t) => {
      if (seen.has(t.toolUrn)) return false;
      seen.add(t.toolUrn);
      return true;
    });
  }, [toolsData, source]);

  const { data: toolsetsData } = useListToolsets();
  const associatedToolsets = useMemo(() => {
    if (!toolsetsData?.toolsets || !relatedTools.length) return [];
    const sourceToolUrns = new Set(relatedTools.map((t) => t.toolUrn));
    return toolsetsData.toolsets.filter((toolset) =>
      toolset.toolUrns?.some((urn) => sourceToolUrns.has(urn)),
    );
  }, [toolsetsData, relatedTools]);

  const sourceToolUrnsArray = useMemo(
    () => relatedTools.map((t) => t.toolUrn),
    [relatedTools],
  );
  const { data: telemetryData, isLoading: isLoadingTelemetry } =
    useLogsEnabledErrorCheck(
      useQuery<GetObservabilityOverviewResult>({
        queryKey: ["source-telemetry", sourceSlug, telemetryFrom.toISOString()],
        queryFn: () =>
          unwrapAsync(
            telemetryGetObservabilityOverview(gramClient, {
              getObservabilityOverviewPayload: {
                from: telemetryFrom,
                to: telemetryTo,
                includeTimeSeries: false,
              },
            }),
          ),
        enabled: relatedTools.length > 0,
        throwOnError: false,
      }),
    );

  const sourceToolMetrics = useMemo(() => {
    if (!telemetryData?.topToolsByCount || sourceToolUrnsArray.length === 0)
      return [];
    const urnSet = new Set(sourceToolUrnsArray);
    return telemetryData.topToolsByCount.filter((m) => urnSet.has(m.gramUrn));
  }, [telemetryData, sourceToolUrnsArray]);

  const sourceTelemetrySummary = useMemo(
    () => computeTelemetrySummary(sourceToolMetrics),
    [sourceToolMetrics],
  );

  const isOpenAPI = sourceKind === "http" || sourceKind === "openapi";
  const sourceType = isOpenAPI ? "OpenAPI" : "Function";

  const { hasScope } = useRBAC();
  const canWriteTools = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const refetchTools = useCallback(
    () => invalidateAllListTools(queryClient),
    [queryClient],
  );
  const { updateTool, isUpdating } = useToolUpdate({
    telemetryEvent: "source_event",
    onSuccess: () => void refetchTools(),
  });

  const uniqueRuntimes = useMemo(() => {
    if (isOpenAPI) return [];
    const runtimes = new Set<string>();
    for (const tool of relatedTools) {
      if (tool.type === "function" && tool.runtime) runtimes.add(tool.runtime);
    }
    return Array.from(runtimes).sort();
  }, [relatedTools, isOpenAPI]);

  const validTabs = useMemo(() => {
    const tabs = ["overview", "tools", "mcp-servers"];
    if (isOpenAPI) tabs.push("spec");
    tabs.push("deployments", "settings");
    return tabs;
  }, [isOpenAPI]);

  useEffect(() => {
    if (!validTabs.includes(activeTab)) {
      setActiveTab("overview");
      const url = new URL(window.location.href);
      url.hash = "overview";
      window.history.replaceState(null, "", url.toString());
    }
  }, [validTabs, activeTab]);

  const handleTabChange = (value: string) => {
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  const {
    data: specContent,
    isLoading: isLoadingSpec,
    error: specError,
    refetch: refetchSpec,
  } = useFetchSourceContent(source ?? null, isOpenAPI, project, projectSlug);

  if (sourceKind === "externalmcp") {
    return <ExternalMCPDetails />;
  }

  if (sourceKind === "remotemcp") {
    return <RemoteMCPDetails />;
  }

  if (sourceKind === "tunneledmcp") {
    return <TunneledMCPDetails />;
  }

  if (!isLoadingDeployment && !source) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={[sourceKind || ""]}
        />
      </Page.Header>

      <Page.Body fullHeight overflowHidden className="gap-0">
        <DetailLayout className="min-h-0 flex-1">
          <DetailLayout.Header
            eyebrow="Source"
            title={source?.name || sourceSlug}
            subtitle={source?.slug}
            actions={
              <Badge variant="neutral">
                <Badge.Text>{sourceType}</Badge.Text>
              </Badge>
            }
          />

          {/* Tabs */}
          <Tabs
            value={activeTab}
            onValueChange={handleTabChange}
            className="flex min-h-0 w-full flex-1 flex-col"
          >
            <DetailLayout.Tabs>
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="tools">
                  Tools {relatedTools.length > 0 && `(${relatedTools.length})`}
                </PageTabsTrigger>
                <PageTabsTrigger value="mcp-servers">
                  MCP Servers
                  {associatedToolsets.length > 0 &&
                    ` (${associatedToolsets.length})`}
                </PageTabsTrigger>
                {isOpenAPI && (
                  <PageTabsTrigger value="spec">
                    OpenAPI Specification
                  </PageTabsTrigger>
                )}
                <PageTabsTrigger value="deployments">
                  Deployments
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </DetailLayout.Tabs>

            <DetailLayout.Content className="min-h-0 flex-1">
              <DetailLayout.Main className="min-h-0">
                <TabsContent value="overview" className="mt-0 flex-1">
                  <SourceOverviewTab
                    source={source ?? null}
                    isOpenAPI={isOpenAPI}
                    underlyingAsset={underlyingAsset}
                    activeDeploymentItem={activeDeploymentItem}
                    sourceToolMetrics={sourceToolMetrics}
                    isLoadingTelemetry={isLoadingTelemetry}
                    sourceTelemetrySummary={sourceTelemetrySummary}
                  />
                </TabsContent>

                <TabsContent
                  value="tools"
                  className="mt-0 flex min-h-0 flex-1 flex-col"
                >
                  <SourceToolsTab
                    relatedTools={relatedTools}
                    isOpenAPI={isOpenAPI}
                    uniqueRuntimes={uniqueRuntimes}
                    onToolUpdate={canWriteTools ? updateTool : undefined}
                    isToolUpdating={isUpdating}
                  />
                </TabsContent>

                <TabsContent value="mcp-servers" className="mt-0 flex-1">
                  <SourceMCPServersTab
                    associatedToolsets={associatedToolsets}
                  />
                </TabsContent>

                {isOpenAPI && (
                  <TabsContent value="spec" className="mt-0">
                    {isLoadingSpec ? (
                      <div className="p-8">
                        <SkeletonCode lines={20} />
                      </div>
                    ) : specError ? (
                      <div className="py-8 text-center">
                        <Type className="text-destructive">
                          {specError instanceof Error
                            ? specError.message
                            : "Failed to fetch spec"}
                        </Type>
                        <Button
                          variant="secondary"
                          size="sm"
                          className="mt-4"
                          onClick={() => {
                            void refetchSpec();
                          }}
                        >
                          <Button.Text>Retry</Button.Text>
                        </Button>
                      </div>
                    ) : specContent ? (
                      <MonacoEditorLazy
                        value={specContent.content}
                        language={specContent.language}
                        height="calc(100vh - 380px)"
                        wordWrap="on"
                      />
                    ) : (
                      <Type className="text-muted-foreground py-8 text-center">
                        No spec content available
                      </Type>
                    )}
                  </TabsContent>
                )}

                <TabsContent
                  value="deployments"
                  className="mt-0 min-h-0 flex-1"
                >
                  <Suspense
                    fallback={<div className="p-8">Loading deployments...</div>}
                  >
                    <SourceDeploymentsPanel
                      sourceKind={sourceKind}
                      attachmentType={attachmentTypeForSourceKind(sourceKind)}
                    />
                  </Suspense>
                </TabsContent>

                <TabsContent value="settings" className="mt-0 flex-1">
                  <SourceSettingsTab
                    isOpenAPI={isOpenAPI}
                    source={source ?? null}
                  />
                </TabsContent>
              </DetailLayout.Main>
            </DetailLayout.Content>
          </Tabs>
        </DetailLayout>
      </Page.Body>
    </Page>
  );
}
