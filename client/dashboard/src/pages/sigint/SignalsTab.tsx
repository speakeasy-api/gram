import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { Button } from "@/components/ui/Button";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { SigintSignal } from "@gram/client/models/components/sigintsignal.js";
import { useCreateSigintSignalMutation } from "@gram/client/react-query/createSigintSignal.js";
import { useDeleteSigintSignalMutation } from "@gram/client/react-query/deleteSigintSignal.js";
import { invalidateAllSigintSensors } from "@gram/client/react-query/sigintSensors.js";
import {
  invalidateAllSigintSignals,
  useSigintSignalsInfinite,
} from "@gram/client/react-query/sigintSignals.js";
import { useUpdateSigintSignalMutation } from "@gram/client/react-query/updateSigintSignal.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { DeleteConfigurationDialog } from "./DeleteConfigurationDialog";
import { SignalEditorDialog, type SignalDraft } from "./SignalEditorDialog";

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function SignalDetails({ signal }: { signal: SigintSignal }): JSX.Element {
  return (
    <div className="grid gap-5 border-t bg-card p-5 md:grid-cols-2">
      <div>
        <p className="text-eyebrow">Description</p>
        <Text className="mt-2 whitespace-pre-wrap" muted={!signal.description}>
          {signal.description || "No description."}
        </Text>
      </div>
      <div>
        <p className="text-eyebrow">Classifier criteria</p>
        <Text
          className="mt-2 whitespace-pre-wrap"
          muted={!signal.classifierCriteria}
        >
          {signal.classifierCriteria || "No classifier criteria."}
        </Text>
      </div>
      <div>
        <p className="text-eyebrow">Signal ID</p>
        <Text className="mt-2 break-all" mono small>
          {signal.id}
        </Text>
      </div>
      <div>
        <p className="text-eyebrow">Updated</p>
        <Text className="mt-2" small>
          {signal.updatedAt.toLocaleString()}
        </Text>
      </div>
    </div>
  );
}

export function SignalsTab(): JSX.Element {
  const queryClient = useQueryClient();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write", project.id);
  const list = useSigintSignalsInfinite({ limit: 50 }, undefined, {
    throwOnError: false,
  });
  const create = useCreateSigintSignalMutation();
  const update = useUpdateSigintSignalMutation();
  const remove = useDeleteSigintSignalMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllSigintSignals(queryClient),
        invalidateAllSigintSensors(queryClient),
      ]);
      toast.success("Signal deleted and detached from sensors");
      setDeleting(null);
    },
  });
  const [search, setSearch] = useState("");
  const [editorSignal, setEditorSignal] = useState<
    SigintSignal | null | undefined
  >();
  const [editorError, setEditorError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<SigintSignal | null>(null);

  const signals = useMemo(
    () => list.data?.pages.flatMap((page) => page.result.signals) ?? [],
    [list.data],
  );
  const filteredSignals = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    if (!needle) return signals;
    return signals.filter(
      (signal) =>
        signal.name.toLocaleLowerCase().includes(needle) ||
        signal.description?.toLocaleLowerCase().includes(needle) ||
        signal.classifierCriteria?.toLocaleLowerCase().includes(needle) ||
        signal.id.toLocaleLowerCase().includes(needle),
    );
  }, [search, signals]);

  const mutationPending =
    create.isPending || update.isPending || remove.isPending;
  const columns: Column<SigintSignal>[] = [
    {
      key: "name",
      header: "Name",
      width: "0.8fr",
      render: (signal) => <Text className="font-medium">{signal.name}</Text>,
    },
    {
      key: "description",
      header: "Description",
      render: (signal) => (
        <Text muted small className="line-clamp-2">
          {signal.description || "No description"}
        </Text>
      ),
    },
    {
      key: "updated",
      header: "Updated",
      width: "180px",
      render: (signal) => (
        <Text muted small>
          {signal.updatedAt.toLocaleDateString()}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (signal) => (
        <RequireScope
          scope="project:write"
          resourceId={project.id}
          level="component"
          reason="You don't have permission to change signals."
        >
          {({ disabled }) => (
            <MoreActions
              triggerAriaLabel={`Actions for ${signal.name}`}
              triggerDisabled={disabled || mutationPending}
              triggerLoading={
                mutationPending &&
                (editorSignal?.id === signal.id || deleting?.id === signal.id)
              }
              actions={[
                {
                  label: "Edit",
                  icon: "pencil",
                  onClick: () => {
                    setEditorError(null);
                    setEditorSignal(signal);
                  },
                },
                {
                  label: "Delete",
                  icon: "trash",
                  destructive: true,
                  onClick: () => setDeleting(signal),
                },
              ]}
            />
          )}
        </RequireScope>
      ),
    },
  ];

  const saveSignal = async (draft: SignalDraft): Promise<void> => {
    if (!canWrite || mutationPending) return;
    setEditorError(null);
    try {
      if (editorSignal) {
        await update.mutateAsync({
          request: {
            updateSigintSignalForm: { id: editorSignal.id, ...draft },
          },
        });
        toast.success("Signal updated");
      } else {
        await create.mutateAsync({
          request: { createSigintSignalForm: draft },
        });
        toast.success("Signal created");
      }
      await invalidateAllSigintSignals(queryClient);
      setEditorSignal(undefined);
    } catch (error) {
      setEditorError(errorMessage(error, "Unable to save the signal."));
    }
  };

  const deleteSignal = async (): Promise<void> => {
    if (!canWrite || !deleting || mutationPending) return;
    try {
      await remove.mutateAsync({ request: { id: deleting.id } });
    } catch (error) {
      toast.error(errorMessage(error, "Unable to delete the signal."));
    }
  };

  let body: JSX.Element;
  if (list.isPending) {
    body = <SkeletonTable />;
  } else if (list.isError && signals.length === 0) {
    body = (
      <div className="flex flex-col items-center gap-3 border py-12 text-center">
        <Text>Unable to load signals.</Text>
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
          data={filteredSignals}
          rowKey={(signal) => signal.id}
          renderExpandedContent={(signal) => <SignalDetails signal={signal} />}
          noResultsMessage={
            <Text muted>
              {search
                ? "No signals match this search."
                : "No signals yet. Create one to build a reusable catalog."}
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
              Unable to load more signals.
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
          placeholder="Search loaded signals"
          debounceMs={150}
        />
        <Page.Toolbar.Actions>
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
            reason="You don't have permission to create signals."
          >
            {({ disabled }) => (
              <Button
                disabled={disabled || mutationPending}
                onClick={() => {
                  setEditorError(null);
                  setEditorSignal(null);
                }}
              >
                <Button.LeftIcon>
                  <Plus />
                </Button.LeftIcon>
                <Button.Text>New signal</Button.Text>
              </Button>
            )}
          </RequireScope>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      {body}

      {editorSignal !== undefined && canWrite ? (
        <SignalEditorDialog
          key={editorSignal?.id ?? "new-signal"}
          open
          signal={editorSignal}
          canWrite={canWrite}
          pending={create.isPending || update.isPending}
          error={editorError}
          onOpenChange={(open) => {
            if (!open) setEditorSignal(undefined);
          }}
          onSave={(draft) => void saveSignal(draft)}
        />
      ) : null}
      {deleting && canWrite ? (
        <DeleteConfigurationDialog
          open
          kind="signal"
          name={deleting.name}
          pending={remove.isPending}
          onOpenChange={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={() => void deleteSignal()}
        />
      ) : null}
    </Stack>
  );
}
