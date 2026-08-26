import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import type { ExploreSavedQuery } from "@gram/client/models/components/exploresavedquery.js";
import { useExploreCreateSavedQueryMutation } from "@gram/client/react-query/exploreCreateSavedQuery.js";
import { useExploreDeleteSavedQueryMutation } from "@gram/client/react-query/exploreDeleteSavedQuery.js";
import {
  invalidateAllExploreListSavedQueries,
  useExploreListSavedQueries,
} from "@gram/client/react-query/exploreListSavedQueries.js";
import { useExploreMeta } from "@gram/client/react-query/exploreMeta.js";
import { useExploreUpdateSavedQueryMutation } from "@gram/client/react-query/exploreUpdateSavedQuery.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ExploreDashboard } from "./ExploreDashboard";
import {
  autoGranularity,
  completeCalcs,
  DEFAULT_SPEC,
  filtersFromDrafts,
  groupExpressionsFromDrafts,
  isTimeseries,
  specFromSavedQuery,
  type ExploreSpec,
} from "./exploreModel";
import { QueryBuilder } from "./QueryBuilder";
import { QueryResults } from "./QueryResults";

type ExploreTab = "dashboard" | "explore";

export function ExplorePage(): JSX.Element {
  return (
    <div className="flex h-full flex-col">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RequireScope scope="org:admin" level="page">
            <ExploreContent />
          </RequireScope>
        </Page.Body>
      </Page>
    </div>
  );
}

function ExploreContent(): JSX.Element {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<ExploreTab>("dashboard");
  const [spec, setSpec] = useState<ExploreSpec>(DEFAULT_SPEC);
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState("");

  const { data: meta } = useExploreMeta();
  const savedQueriesQuery = useExploreListSavedQueries();
  const savedQueries = savedQueriesQuery.data?.queries ?? [];
  const hasCalculations = completeCalcs(spec.calculations).length > 0;

  function invalidateSavedQueries(): Promise<void> {
    return invalidateAllExploreListSavedQueries(queryClient);
  }

  const createSavedQuery = useExploreCreateSavedQueryMutation({
    onSuccess: (created) => {
      void invalidateSavedQueries();
      setSpec((current) => ({
        ...current,
        loadedQueryId: created.id,
        name: created.name,
      }));
      setSaveOpen(false);
      setTab("dashboard");
    },
  });
  const updateSavedQuery = useExploreUpdateSavedQueryMutation({
    onSuccess: () => {
      void invalidateSavedQueries();
      setTab("dashboard");
    },
  });
  const deleteSavedQuery = useExploreDeleteSavedQueryMutation({
    onSuccess: () => void invalidateSavedQueries(),
  });
  const saving = createSavedQuery.isPending || updateSavedQuery.isPending;

  const savedQueryBody = (name: string) => ({
    name,
    chartType: spec.chartType,
    window: spec.window,
    dataset: spec.dataset,
    calculations: completeCalcs(spec.calculations).map((calculation) => ({
      op: calculation.op,
      column: calculation.column || undefined,
    })),
    groupBy: spec.chartType === "number" ? [] : spec.groupBy,
    groupExpressions:
      spec.chartType === "number"
        ? []
        : groupExpressionsFromDrafts(spec.groupExpressions),
    filters: filtersFromDrafts(spec.filters),
    granularitySeconds: isTimeseries(spec.chartType)
      ? autoGranularity(spec.window)
      : 0,
    sortBy: spec.orderBy || undefined,
    sortDesc: true,
    limit: spec.limit,
  });

  const editQuery = (query: ExploreSavedQuery) => {
    setSpec(specFromSavedQuery(query));
    setTab("explore");
  };
  const deleteQuery = (query: ExploreSavedQuery) => {
    deleteSavedQuery.mutate(
      { request: { id: query.id } },
      {
        onSuccess: () =>
          setSpec((current) =>
            current.loadedQueryId === query.id ? DEFAULT_SPEC : current,
          ),
      },
    );
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="preview">Explore</Page.Section.Title>
        <Page.Section.Description>
          Build charts from your AI usage and save them to a shared dashboard.
        </Page.Section.Description>
        <Page.Section.CTA>
          {tab === "explore" ? (
            <div className="flex items-center gap-2">
              {spec.loadedQueryId ? (
                <Button
                  variant="secondary"
                  size="sm"
                  icon="save"
                  disabled={!hasCalculations || saving}
                  onClick={() =>
                    updateSavedQuery.mutate({
                      request: {
                        updateSavedQueryRequestBody: {
                          id: spec.loadedQueryId ?? "",
                          ...savedQueryBody(spec.name || "Untitled query"),
                        },
                      },
                    })
                  }
                >
                  Update query
                </Button>
              ) : null}
              <Button
                variant="primary"
                size="sm"
                icon="bookmark-plus"
                disabled={!hasCalculations || saving}
                onClick={() => {
                  setSaveName(spec.name);
                  setSaveOpen(true);
                }}
              >
                Save to dashboard
              </Button>
            </div>
          ) : null}
        </Page.Section.CTA>
        <Page.Section.Body>
          <Tabs
            value={tab}
            onValueChange={(value) => setTab(value as ExploreTab)}
            className="gap-4"
          >
            <div className="border-border border-b">
              <PageTabsList>
                <PageTabsTrigger value="dashboard">Dashboard</PageTabsTrigger>
                <PageTabsTrigger value="explore">Explore</PageTabsTrigger>
              </PageTabsList>
            </div>
            <TabsContent value="dashboard">
              <ExploreDashboard
                queries={savedQueries}
                loading={savedQueriesQuery.isPending}
                meta={meta}
                onEdit={editQuery}
                onDelete={deleteQuery}
                onBuild={() => setTab("explore")}
              />
            </TabsContent>
            <TabsContent value="explore">
              <div className="flex flex-col gap-4">
                <QueryBuilder meta={meta} spec={spec} onChange={setSpec} />
                <QueryResults meta={meta} spec={spec} />
              </div>
            </TabsContent>
          </Tabs>
        </Page.Section.Body>
      </Page.Section>

      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <Dialog.Content>
          <Dialog.Title>Save to dashboard</Dialog.Title>
          <Dialog.Description>
            Save this query as a live dashboard chart.
          </Dialog.Description>
          <Input
            value={saveName}
            onChange={setSaveName}
            placeholder="e.g. Cost by response model, last 7 days"
            autoFocus
          />
          <Dialog.Footer>
            <Button variant="tertiary" onClick={() => setSaveOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              disabled={saveName.trim() === "" || saving}
              onClick={() =>
                createSavedQuery.mutate({
                  request: {
                    createSavedQueryRequestBody: savedQueryBody(
                      saveName.trim(),
                    ),
                  },
                })
              }
            >
              Save
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
