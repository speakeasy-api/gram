import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type {
  SigintSensor,
  SigintSensorMode,
} from "@gram/client/models/components/sigintsensor.js";
import type { SigintSignal } from "@gram/client/models/components/sigintsignal.js";
import { useCreateSigintSensorMutation } from "@gram/client/react-query/createSigintSensor.js";
import { useDeleteSigintSensorMutation } from "@gram/client/react-query/deleteSigintSensor.js";
import {
  invalidateAllSigintSensors,
  useSigintSensorsInfinite,
} from "@gram/client/react-query/sigintSensors.js";
import { useSigintSignalsInfinite } from "@gram/client/react-query/sigintSignals.js";
import { useUpdateSigintSensorMutation } from "@gram/client/react-query/updateSigintSensor.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { DeleteConfigurationDialog } from "./DeleteConfigurationDialog";
import { SensorEditorDialog, type SensorDraft } from "./SensorEditorDialog";

const MODE_LABELS: Record<SigintSensorMode, string> = {
  multi_label: "Multi-label",
  exclusive: "Exclusive",
  ordered_score: "Ordered score",
};

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function duplicateNameCounts(signals: SigintSignal[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const signal of signals) {
    counts.set(signal.name, (counts.get(signal.name) ?? 0) + 1);
  }
  return counts;
}

function SensorDetails({
  sensor,
  signalsById,
  nameCounts,
}: {
  sensor: SigintSensor;
  signalsById: Map<string, SigintSignal>;
  nameCounts: Map<string, number>;
}): JSX.Element {
  return (
    <div className="space-y-5 border-t bg-card p-5">
      <div className="grid gap-5 md:grid-cols-2">
        <div>
          <p className="text-eyebrow">Description</p>
          <Text
            className="mt-2 whitespace-pre-wrap"
            muted={!sensor.description}
          >
            {sensor.description || "No description."}
          </Text>
        </div>
        <div>
          <p className="text-eyebrow">Instructions</p>
          <Text
            className="mt-2 whitespace-pre-wrap"
            muted={!sensor.instructions}
          >
            {sensor.instructions || "No classification instructions."}
          </Text>
        </div>
      </div>
      <div>
        <p className="text-eyebrow">Ordered signal membership</p>
        {sensor.signalIds.length === 0 ? (
          <Text muted className="mt-2">
            No signals attached. This is a valid draft.
          </Text>
        ) : (
          <div className="mt-2 divide-y border">
            {sensor.signalIds.map((id, index) => {
              const signal = signalsById.get(id);
              const duplicate =
                signal && (nameCounts.get(signal.name) ?? 0) > 1;
              return (
                <div key={id} className="flex items-center gap-3 px-3 py-2">
                  {sensor.mode === "ordered_score" ? (
                    <span
                      className="w-6 shrink-0 font-mono text-sm"
                      aria-label={`Score level ${index}`}
                    >
                      {index}
                    </span>
                  ) : null}
                  <span className="min-w-0 flex-1 text-sm">
                    {signal?.name ?? "Signal not loaded"}
                    {duplicate ? (
                      <span className="text-muted-foreground ml-2 font-mono text-xs">
                        {id}
                      </span>
                    ) : null}
                  </span>
                  {!signal ? (
                    <span className="text-muted-foreground font-mono text-xs">
                      {id}
                    </span>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </div>
      <div className="grid gap-5 md:grid-cols-2">
        <div>
          <p className="text-eyebrow">Sensor ID</p>
          <Text className="mt-2 break-all" mono small>
            {sensor.id}
          </Text>
        </div>
        <div>
          <p className="text-eyebrow">Updated</p>
          <Text className="mt-2" small>
            {sensor.updatedAt.toLocaleString()}
          </Text>
        </div>
      </div>
    </div>
  );
}

export function SensorsTab(): JSX.Element {
  const queryClient = useQueryClient();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write", project.id);
  const list = useSigintSensorsInfinite({ limit: 50 }, undefined, {
    throwOnError: false,
  });
  const signalCatalog = useSigintSignalsInfinite({ limit: 50 }, undefined, {
    throwOnError: false,
  });
  const { hasNextPage, isFetching, isError, fetchNextPage } = signalCatalog;
  useEffect(() => {
    if (hasNextPage && !isFetching && !isError) void fetchNextPage();
  }, [hasNextPage, isFetching, isError, fetchNextPage]);
  const create = useCreateSigintSensorMutation();
  const update = useUpdateSigintSensorMutation();
  const remove = useDeleteSigintSensorMutation();
  const [search, setSearch] = useState("");
  const [editorSensor, setEditorSensor] = useState<
    SigintSensor | null | undefined
  >();
  const [editorError, setEditorError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<SigintSensor | null>(null);

  const sensors = useMemo(
    () => list.data?.pages.flatMap((page) => page.result.sensors) ?? [],
    [list.data],
  );
  const signals = useMemo(
    () =>
      signalCatalog.data?.pages.flatMap((page) => page.result.signals) ?? [],
    [signalCatalog.data],
  );
  const signalsById = useMemo(
    () => new Map(signals.map((signal) => [signal.id, signal])),
    [signals],
  );
  const nameCounts = useMemo(() => duplicateNameCounts(signals), [signals]);
  const filteredSensors = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    if (!needle) return sensors;
    return sensors.filter(
      (sensor) =>
        sensor.name.toLocaleLowerCase().includes(needle) ||
        sensor.description?.toLocaleLowerCase().includes(needle) ||
        sensor.instructions?.toLocaleLowerCase().includes(needle) ||
        sensor.id.toLocaleLowerCase().includes(needle),
    );
  }, [search, sensors]);

  const mutationPending =
    create.isPending || update.isPending || remove.isPending;
  const columns: Column<SigintSensor>[] = [
    {
      key: "name",
      header: "Name",
      width: "0.8fr",
      render: (sensor) => <Text className="font-medium">{sensor.name}</Text>,
    },
    {
      key: "mode",
      header: "Mode",
      width: "160px",
      render: (sensor) => <Text small>{MODE_LABELS[sensor.mode]}</Text>,
    },
    {
      key: "signals",
      header: "Signals",
      width: "100px",
      render: (sensor) => <Text small>{sensor.signalIds.length}</Text>,
    },
    {
      key: "description",
      header: "Description",
      render: (sensor) => (
        <Text muted small className="line-clamp-2">
          {sensor.description || "No description"}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (sensor) => (
        <RequireScope
          scope="project:write"
          resourceId={project.id}
          level="component"
          reason="You don't have permission to change sensors."
        >
          {({ disabled }) => (
            <MoreActions
              triggerAriaLabel={`Actions for ${sensor.name}`}
              triggerDisabled={disabled || mutationPending}
              triggerLoading={
                mutationPending &&
                (editorSensor?.id === sensor.id || deleting?.id === sensor.id)
              }
              actions={[
                {
                  label: "Edit",
                  icon: "pencil",
                  onClick: () => {
                    setEditorError(null);
                    setEditorSensor(sensor);
                  },
                },
                {
                  label: "Delete",
                  icon: "trash",
                  destructive: true,
                  onClick: () => setDeleting(sensor),
                },
              ]}
            />
          )}
        </RequireScope>
      ),
    },
  ];

  const saveSensor = async (draft: SensorDraft): Promise<void> => {
    if (!canWrite || mutationPending) return;
    setEditorError(null);
    try {
      if (editorSensor) {
        await update.mutateAsync({
          request: {
            updateSigintSensorForm: { id: editorSensor.id, ...draft },
          },
        });
        toast.success("Sensor updated");
      } else {
        await create.mutateAsync({
          request: { createSigintSensorForm: draft },
        });
        toast.success("Sensor created");
      }
      await invalidateAllSigintSensors(queryClient);
      setEditorSensor(undefined);
    } catch (error) {
      setEditorError(errorMessage(error, "Unable to save the sensor."));
    }
  };

  const deleteSensor = async (): Promise<void> => {
    if (!canWrite || !deleting || mutationPending) return;
    try {
      await remove.mutateAsync({ request: { id: deleting.id } });
      await invalidateAllSigintSensors(queryClient);
      toast.success("Sensor deleted");
      setDeleting(null);
    } catch (error) {
      toast.error(errorMessage(error, "Unable to delete the sensor."));
    }
  };

  let body: JSX.Element;
  if (list.isPending) {
    body = <SkeletonTable />;
  } else if (list.isError && sensors.length === 0) {
    body = (
      <div className="flex flex-col items-center gap-3 border py-12 text-center">
        <Text>Unable to load sensors.</Text>
        <Text muted small>
          {list.error.message}
        </Text>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void list.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else {
    body = (
      <>
        <Table
          columns={columns}
          data={filteredSensors}
          rowKey={(sensor) => sensor.id}
          renderExpandedContent={(sensor) => (
            <SensorDetails
              sensor={sensor}
              signalsById={signalsById}
              nameCounts={nameCounts}
            />
          )}
          noResultsMessage={
            <Text muted>
              {search
                ? "No sensors match this search."
                : "No sensors yet. Create one to compose signals into classification configuration."}
            </Text>
          }
          hasMore={list.hasNextPage}
          onLoadMore={async () => {
            await list.fetchNextPage();
          }}
        />
        {list.isFetchNextPageError ? (
          <div className="mt-3 flex items-center justify-between border p-3">
            <Text destructive small>
              Unable to load more sensors.
            </Text>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void list.fetchNextPage()}
            >
              Retry
            </Button>
          </div>
        ) : null}
      </>
    );
  }

  return (
    <Stack gap={2} className="mt-3 mb-6">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          placeholder="Search loaded sensors"
          debounceMs={150}
        />
        <Page.Toolbar.Actions>
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
            reason="You don't have permission to create sensors."
          >
            {({ disabled }) => (
              <Button
                disabled={disabled || mutationPending}
                onClick={() => {
                  setEditorError(null);
                  setEditorSensor(null);
                }}
              >
                <Button.LeftIcon>
                  <Plus />
                </Button.LeftIcon>
                <Button.Text>New sensor</Button.Text>
              </Button>
            )}
          </RequireScope>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      {signalCatalog.isError ? (
        <div className="flex items-center justify-between gap-3 border p-3">
          <Text destructive small>
            Could not load the signal catalog. Membership names may be
            incomplete.
          </Text>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void signalCatalog.refetch()}
          >
            Retry
          </Button>
        </div>
      ) : null}
      {body}

      {editorSensor !== undefined && canWrite ? (
        <SensorEditorDialog
          key={editorSensor?.id ?? "new-sensor"}
          open
          canWrite={canWrite}
          sensor={editorSensor}
          signals={signals}
          catalogLoading={signalCatalog.isPending}
          catalogError={signalCatalog.isError}
          hasMoreSignals={signalCatalog.hasNextPage}
          loadingMoreSignals={signalCatalog.isFetchingNextPage}
          pending={create.isPending || update.isPending}
          error={editorError}
          onLoadMoreSignals={() => void signalCatalog.fetchNextPage()}
          onRetrySignals={() => void signalCatalog.refetch()}
          onOpenChange={(open) => {
            if (!open) setEditorSensor(undefined);
          }}
          onSave={(draft) => void saveSensor(draft)}
        />
      ) : null}
      {deleting && canWrite ? (
        <DeleteConfigurationDialog
          open
          kind="sensor"
          name={deleting.name}
          pending={remove.isPending}
          onOpenChange={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={() => void deleteSensor()}
        />
      ) : null}
    </Stack>
  );
}
