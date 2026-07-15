import {
  EnvironmentVariableDialog,
  type EnvironmentVariableDraft,
} from "@/components/environments/EnvironmentVariableDialog";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import {
  useRegisterEnvironmentTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { useDeleteEnvironmentMutation } from "@gram/client/react-query/deleteEnvironment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateEnvironmentMutation } from "@gram/client/react-query/updateEnvironment.js";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { AlertCircle, Eye, EyeOff, Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { type Action, MoreActions } from "@/components/ui/more-actions";
import { useEnvironment } from "./useEnvironment";

const MASK = "••••••••••••";

interface ToolsetDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (toolsetSlug: string) => void;
}

function ToolsetDialog({ open, onOpenChange, onSubmit }: ToolsetDialogProps) {
  const { data: toolsetsData } = useListToolsets();
  const [selectedToolset, setSelectedToolset] = useState<string>("");

  useEffect(() => {
    if (!open) {
      setSelectedToolset("");
    }
  }, [open]);

  const handleClose = () => {
    onOpenChange(false);
    setSelectedToolset("");
  };

  const handleSubmit = () => {
    onSubmit(selectedToolset);
    handleClose();
  };

  const options =
    toolsetsData?.toolsets.map((toolset) => ({
      label: toolset.name,
      value: toolset.slug,
    })) || [];

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Fill for MCP Server</Dialog.Title>
          <Dialog.Description>
            <p>
              Select an MCP server you would like to prefill environment
              variables for. All relevant env variables will be filled in with
              empty placeholders.
            </p>
            <br />
            <p>
              When an API has multiple optional security options, you only need
              to provide values for the security scheme relevant to you and you
              can remove the uneeded entries.
            </p>
            <br />
            <p>
              If your API has a default server URL, providing a value for a
              server URL is not required.
            </p>
          </Dialog.Description>
        </Dialog.Header>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Type>MCP Server</Type>
            <select
              value={selectedToolset}
              onChange={(e) => setSelectedToolset(e.target.value)}
              className="border-input placeholder:text-muted-foreground focus-visible:ring-ring flex h-9 w-full rounded-md border bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:ring-1 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select an MCP server</option>
              {options.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!selectedToolset}>
            Fill Variables
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

interface EnvironmentVariableRowProps {
  entry: EnvironmentVariableDraft;
  canWrite: boolean;
  revealed: boolean;
  disabled: boolean;
  onToggleReveal: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function EnvironmentVariableRow({
  entry,
  canWrite,
  revealed,
  disabled,
  onToggleReveal,
  onEdit,
  onDelete,
}: EnvironmentVariableRowProps) {
  const actions: Action[] = [];
  if (canWrite) {
    actions.push({
      label: "Edit",
      onClick: onEdit,
      icon: "pencil",
      disabled: disabled,
    });
  }
  // A secret only ever hands back its redacted preview, so copying one would
  // put a useless string on the clipboard. Keep the row shape steady by
  // showing the action locked rather than dropping it.
  actions.push({
    label: "Copy to Clipboard",
    onClick: () => {
      void navigator.clipboard.writeText(entry.value);
    },
    icon: entry.isSecret ? "lock" : "copy",
    disabled: entry.isSecret,
  });
  if (canWrite) {
    actions.push({
      label: "Delete",
      onClick: onDelete,
      icon: "trash",
      destructive: true,
      disabled: disabled,
    });
  }

  return (
    <div className="flex items-center gap-3 rounded-md border px-3 py-2">
      <div className="grid flex-1 grid-cols-2 items-center gap-4">
        <div className="flex">
          <span className="text-foreground font-mono text-sm font-medium">
            {entry.name}
          </span>
          {entry.isSecret && (
            <Badge
              variant="neutral"
              size="sm"
              className="ml-2 h-4 px-1 text-xs"
            >
              Sensitive
            </Badge>
          )}
        </div>
        <div className="flex w-full items-center justify-end gap-2">
          <span className="text-muted-foreground flex-1 truncate font-mono text-sm">
            {revealed ? entry.value : MASK}
          </span>
          <Button
            variant="tertiary"
            size="sm"
            className="h-8 w-8 flex-shrink-0"
            onClick={onToggleReveal}
            aria-label={revealed ? `Hide ${entry.name}` : `View ${entry.name}`}
          >
            <Button.LeftIcon>
              {revealed ? (
                <EyeOff className="h-4 w-4" />
              ) : (
                <Eye className="h-4 w-4" />
              )}
            </Button.LeftIcon>
          </Button>
        </div>
      </div>
      <MoreActions actions={actions} />
    </div>
  );
}

export default function EnvironmentPage(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <EnvironmentPageInner />
    </RequireScope>
  );
}

function EnvironmentPageInner() {
  const environment = useEnvironment();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("environment:write");
  // "Fill for MCP Server" links an environment to a toolset, which remains project:write.
  const canLinkToolset = hasScope("project:write");

  const [toolsetDialogOpen, setToolsetDialogOpen] = useState(false);
  const [selectedToolsetSlug, setSelectedToolsetSlug] = useState<string>("");
  const [revealedFields, setRevealedFields] = useState<Set<string>>(new Set());
  const [pageError, setPageError] = useState<string | null>(null);
  const [variableDialog, setVariableDialog] = useState<{
    open: boolean;
    entry?: EnvironmentVariableDraft;
  }>({ open: false });
  const [deleteConfirmDialog, setDeleteConfirmDialog] = useState<{
    open: boolean;
    varName: string;
  }>({ open: false, varName: "" });

  useRegisterEnvironmentTelemetry({
    environmentSlug: environment?.slug ?? "",
  });

  const deleteEnvironmentMutation = useDeleteEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_deleted",
      });
      void environment!.refetch();
      void navigate("/environments");
    },
  });

  const { mutate: updateEnvironment, isPending: isMutating } =
    useUpdateEnvironmentMutation({
      onSuccess: () => {
        telemetry.capture("environment_event", {
          action: "environment_updated",
        });
        void environment!.refetch();
        setPageError(null);
      },
      onError: (error) => {
        console.error(
          "Environment variable save failed:",
          error?.message || error,
        );
        setPageError("Failed to save environment variables. Please try again.");
      },
    });

  useEffect(() => {
    setRevealedFields(new Set());
    setPageError(null);
  }, [environment?.slug]);

  const { data: selectedToolset } = useToolset(
    { slug: selectedToolsetSlug },
    undefined,
    { enabled: !!selectedToolsetSlug },
  );

  // "Fill for MCP Server" writes empty placeholder entries straight through.
  // They are created secret because the column rejects an empty plaintext
  // value, while a secret entry stores ciphertext that is never empty.
  useEffect(() => {
    if (!selectedToolset || !environment) return;

    const names = new Set<string>();
    selectedToolset.securityVariables?.forEach((entry) => {
      entry.envVariables.forEach((varName) => {
        names.add(varName);
      });
    });
    selectedToolset.serverVariables?.forEach((entry) => {
      entry.envVariables.forEach((varName) => {
        names.add(varName);
      });
    });
    selectedToolset.functionEnvironmentVariables?.forEach((entry) => {
      names.add(entry.name);
    });
    selectedToolset.externalMcpHeaderDefinitions?.forEach((entry) => {
      names.add(entry.name);
    });

    const entriesToUpdate = Array.from(names)
      .filter((name) => !environment.entries?.some((e) => e.name === name))
      .map((name) => ({ name, value: "", isSecret: true }));

    setSelectedToolsetSlug("");
    if (entriesToUpdate.length === 0) return;

    updateEnvironment({
      request: {
        slug: environment.slug,
        updateEnvironmentRequestBody: { entriesToUpdate, entriesToRemove: [] },
      },
    });
  }, [selectedToolset, environment, updateEnvironment]);

  const handleToggleReveal = useCallback((varName: string) => {
    setRevealedFields((prev) => {
      const next = new Set(prev);
      if (next.has(varName)) {
        next.delete(varName);
      } else {
        next.add(varName);
      }
      return next;
    });
  }, []);

  const confirmDelete = useCallback(
    (varName: string) => {
      if (!environment) return;
      updateEnvironment({
        request: {
          slug: environment.slug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [],
            entriesToRemove: [varName],
          },
        },
      });
      setDeleteConfirmDialog({ open: false, varName: "" });
    },
    [environment, updateEnvironment],
  );

  const handleSaved = useCallback(() => {
    void environment?.refetch();
  }, [environment]);

  if (!environment) {
    return <div>Environment not found</div>;
  }

  const entries = environment.entries ?? [];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>{environment.name}</Page.Section.Title>
          <Page.Section.CTA>
            <RequireScope scope="environment:write" level="component">
              <Button
                onClick={() => setVariableDialog({ open: true })}
                disabled={isMutating}
              >
                Add Variable
              </Button>
            </RequireScope>
          </Page.Section.CTA>
          <MoreActions
            actions={[
              {
                label: "Fill for MCP Server",
                onClick: () => setToolsetDialogOpen(true),
                icon: "copy-plus",
                disabled: !canLinkToolset,
              },
              {
                label: "Delete Environment",
                onClick: () =>
                  deleteEnvironmentMutation.mutate({
                    request: { slug: environment.slug },
                  }),
                icon: "trash",
                destructive: true,
                disabled: !canWrite,
              },
            ]}
          />
          <Page.Section.Body>
            <div className="space-y-6">
              {pageError && (
                <div
                  className="text-destructive flex items-center gap-2 text-sm"
                  role="alert"
                >
                  <AlertCircle className="h-4 w-4" aria-hidden="true" />
                  {pageError}
                </div>
              )}

              <div className="space-y-2">
                {entries.map((entry) => (
                  <EnvironmentVariableRow
                    key={entry.name}
                    entry={entry}
                    canWrite={canWrite}
                    revealed={revealedFields.has(entry.name)}
                    disabled={isMutating}
                    onToggleReveal={() => handleToggleReveal(entry.name)}
                    onEdit={() =>
                      setVariableDialog({
                        open: true,
                        entry: {
                          name: entry.name,
                          value: entry.value,
                          isSecret: entry.isSecret,
                        },
                      })
                    }
                    onDelete={() =>
                      setDeleteConfirmDialog({
                        open: true,
                        varName: entry.name,
                      })
                    }
                  />
                ))}
              </div>

              {entries.length === 0 && (
                <div className="py-8 text-center">
                  <p className="text-muted-foreground text-sm">
                    No environment variables defined
                  </p>
                  {canWrite && (
                    <div className="mt-4 flex flex-col items-center gap-2">
                      <Button onClick={() => setVariableDialog({ open: true })}>
                        <Plus className="mr-2 h-4 w-4" />
                        ADD YOUR FIRST VARIABLE
                      </Button>
                      <Button
                        variant="secondary"
                        onClick={() => setToolsetDialogOpen(true)}
                      >
                        FILL FOR TOOLSET
                      </Button>
                    </div>
                  )}
                </div>
              )}
            </div>

            <EnvironmentVariableDialog
              open={variableDialog.open}
              onOpenChange={(open) =>
                setVariableDialog((prev) => (open ? prev : { open: false }))
              }
              environmentSlug={environment.slug}
              entry={variableDialog.entry}
              existingNames={entries.map((entry) => entry.name)}
              onSaved={handleSaved}
            />

            <ToolsetDialog
              open={toolsetDialogOpen}
              onOpenChange={setToolsetDialogOpen}
              onSubmit={setSelectedToolsetSlug}
            />

            <Dialog
              open={deleteConfirmDialog.open}
              onOpenChange={(open) => {
                void (
                  !open && setDeleteConfirmDialog({ open: false, varName: "" })
                );
              }}
            >
              <Dialog.Content>
                <Dialog.Header>
                  <Dialog.Title>Delete Environment Variable</Dialog.Title>
                  <Dialog.Description>
                    Are you sure you want to delete{" "}
                    <strong>{deleteConfirmDialog.varName}</strong>? This action
                    is permanent.
                  </Dialog.Description>
                </Dialog.Header>
                <Dialog.Footer>
                  <Button
                    variant="tertiary"
                    onClick={() =>
                      setDeleteConfirmDialog({ open: false, varName: "" })
                    }
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="destructive-primary"
                    onClick={() => confirmDelete(deleteConfirmDialog.varName)}
                  >
                    Delete
                  </Button>
                </Dialog.Footer>
              </Dialog.Content>
            </Dialog>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
