import MonacoEditorLazy from "@/components/monaco-editor.lazy";
import { Page } from "@/components/page-layout";
import { MCPPatternIllustration } from "@/components/sources/SourceCardIllustrations";
import { useFetchSourceContent } from "@/components/sources/ViewSourceDialogContent";
import { SkeletonCode } from "@/components/ui/skeleton";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListAssets,
  useListDeployments,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { useGramContext } from "@gram/client/react-query/_context";
import { useQuery } from "@tanstack/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components";
import { useListTools } from "@/hooks/toolTypes";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { Suspense, useEffect, useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { SourceDeploymentsPanel } from "./SourceDeploymentsPanel";
import ExternalMCPDetails from "./external-mcp/ExternalMCPDetails";
import { SourceOverviewTab } from "./SourceOverviewTab";
import { SourceToolsTab } from "./SourceToolsTab";
import { SourceMCPServersTab } from "./SourceMCPServersTab";
import { SourceSettingsTab } from "./SourceSettingsTab";

export default function SourceDetails() {
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
    useQuery<GetObservabilityOverviewResult>({
      queryKey: ["source-telemetry", sourceSlug, telemetryFrom.toISOString()],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(gramClient, {
            getObservabilityOverviewPayload: {
              from: telemetryFrom,
              to: telemetryTo,
              includeTimeSeries: true,
            },
          }),
        ),
      enabled: relatedTools.length > 0,
    });

  const sourceToolMetrics = useMemo(() => {
    if (!telemetryData?.topToolsByCount || sourceToolUrnsArray.length === 0)
      return [];
    const urnSet = new Set(sourceToolUrnsArray);
    return telemetryData.topToolsByCount.filter((m) => urnSet.has(m.gramUrn));
  }, [telemetryData, sourceToolUrnsArray]);

  const sourceTelemetrySummary = useMemo(() => {
    if (sourceToolMetrics.length === 0) return null;
    const totalCalls = sourceToolMetrics.reduce(
      (sum, m) => sum + m.callCount,
      0,
    );
    const totalFailures = sourceToolMetrics.reduce(
      (sum, m) => sum + m.failureCount,
      0,
    );
    const avgLatency =
      totalCalls > 0
        ? sourceToolMetrics.reduce(
            (sum, m) => sum + m.avgLatencyMs * m.callCount,
            0,
          ) / totalCalls
        : 0;
    const errorRate = totalCalls > 0 ? (totalFailures / totalCalls) * 100 : 0;
    return { totalCalls, totalFailures, avgLatency, errorRate };
  }, [sourceToolMetrics]);

  const isOpenAPI = sourceKind === "http" || sourceKind === "openapi";
  const sourceType = isOpenAPI ? "OpenAPI" : "Function";

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
      window.location.hash = "overview";
    }
  }, [validTabs, activeTab]);

  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.replace("#", "");
      if (validTabs.includes(hash)) setActiveTab(hash);
    };
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, [validTabs]);

  const handleTabChange = (value: string) => {
    setActiveTab(value);
    window.location.hash = value;
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

      <Page.Body fullWidth noPadding fullHeight overflowHidden>
        {/* Hero Header */}
        <div className="relative w-full h-64 shrink-0 overflow-hidden">
          <MCPPatternIllustration
            toolsetSlug={sourceSlug || ""}
            className="saturate-[.3]"
          />
          <div className="absolute inset-0 bg-linear-to-t from-foreground/50 via-foreground/20 to-transparent" />
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1" className="text-background">
                  {source?.name || sourceSlug}
                </Heading>
                <Badge variant="neutral">
                  <Badge.Text>{sourceType}</Badge.Text>
                </Badge>
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-background/70!">
                  {source?.slug}
                </Type>
              </div>
            </div>
          </div>
        </div>

        {/* Tabs */}
        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="w-full flex-1 flex flex-col min-h-0"
        >
          <div className="border-b shrink-0">
            <div className="max-w-[1270px] mx-auto px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none">
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
            </div>
          </div>

          <TabsContent value="overview" className="mt-0 flex-1">
            <SourceOverviewTab
              source={source ?? null}
              isOpenAPI={isOpenAPI}
              underlyingAsset={underlyingAsset}
              activeDeploymentItem={activeDeploymentItem}
              telemetryData={telemetryData}
              isLoadingTelemetry={isLoadingTelemetry}
              sourceTelemetrySummary={sourceTelemetrySummary}
            />
          </TabsContent>

          <TabsContent
            value="tools"
            className="mt-0 flex-1 flex flex-col min-h-0"
          >
            <SourceToolsTab
              relatedTools={relatedTools}
              isOpenAPI={isOpenAPI}
              uniqueRuntimes={uniqueRuntimes}
            />
          </TabsContent>

          <TabsContent value="mcp-servers" className="mt-0 flex-1">
            <SourceMCPServersTab associatedToolsets={associatedToolsets} />
          </TabsContent>

          {isOpenAPI && (
            <TabsContent value="spec" className="mt-0">
              {isLoadingSpec ? (
                <div className="p-8">
                  <SkeletonCode lines={20} />
                </div>
              ) : specError ? (
                <div className="text-center py-8">
                  <Type className="text-destructive">
                    {specError instanceof Error
                      ? specError.message
                      : "Failed to fetch spec"}
                  </Type>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="mt-4"
                    onClick={() => refetchSpec()}
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
                <Type className="text-muted-foreground text-center py-8">
                  No spec content available
                </Type>
              )}
            </TabsContent>
          )}

          <TabsContent value="deployments" className="mt-0 flex-1 min-h-0">
            <Suspense
              fallback={<div className="p-8">Loading deployments...</div>}
            >
              <SourceDeploymentsPanel
                sourceKind={sourceKind}
                attachmentType={
                  sourceKind === "function" ? "function" : "openapi"
                }
              />
            </Suspense>
          </TabsContent>

          <TabsContent value="settings" className="mt-0 flex-1">
            <SourceSettingsTab isOpenAPI={isOpenAPI} source={source ?? null} />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}
