import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import {
  useRegisterEnvironmentTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import {
  useDeleteEnvironmentMutation,
  useListToolsets,
  useToolset,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query/index.js";
import { AlertCircle, Eye, EyeOff, Plus, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useEnvironments } from "./Environments";
import { MoreActions } from "@/components/ui/more-actions";

interface SaveActionBarProps {
  saveError: string | null;
  isSaving: boolean;
  onSave: () => void;
  onCancel: () => void;
}

function SaveActionBar({
  saveError,
  isSaving,
  onSave,
  onCancel,
}: SaveActionBarProps) {
  return (
    <div className="flex items-center justify-between pt-4 border-t">
      {saveError && (
        <div
          className="flex items-center gap-2 text-sm text-destructive"
          role="alert"
        >
          <AlertCircle className="h-4 w-4" aria-hidden="true" />
          {saveError}
        </div>
      )}
      <div className="flex items-center gap-3 ml-auto">
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={onCancel}
          disabled={isSaving}
          aria-label="Cancel changes"
        >
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={onSave}
          disabled={isSaving}
          aria-label={
            isSaving
              ? "Saving environment variables"
              : "Save environment variables"
          }
        >
          {isSaving ? "Saving..." : "Save"}
        </Button>
      </div>
    </div>
  );
}

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
          <Dialog.Title>Fill for Toolset</Dialog.Title>
          <Dialog.Description>
            <p>
              Select a toolsets you would like to prefill environment variables
              for. All relevant env variables will be filled in with empty
              placeholders.
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
            <Type>Toolset</Type>
            <select
              value={selectedToolset}
              onChange={(e) => setSelectedToolset(e.target.value)}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select a toolset</option>
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

export function useEnvironment(slug?: string) {
  let { environmentSlug } = useParams();
  if (slug) environmentSlug = slug;

  const environments = useEnvironments();

  const environment = environments.find(
    (environment) => environment.slug === environmentSlug,
  );

  return environment
    ? Object.assign(environment, { refetch: environments.refetch })
    : null;
}

export default function EnvironmentPage() {
  const environment = useEnvironment();
  const navigate = useNavigate();
  const telemetry = useTelemetry();

  const [toolsetDialogOpen, setToolsetDialogOpen] = useState(false);
  const [selectedToolsetSlug, setSelectedToolsetSlug] = useState<string>("");
  const [envValues, setEnvValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);
  const [editedFields, setEditedFields] = useState<Set<string>>(new Set());
  const [deletedFields, setDeletedFields] = useState<Set<string>>(new Set());
  const [focusedField, setFocusedField] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isAddingNew, setIsAddingNew] = useState(false);
  const [newEntryName, setNewEntryName] = useState("");
  const [newEntryValue, setNewEntryValue] = useState("");
  const [newEntryVisible, setNewEntryVisible] = useState(false);
  const [deleteConfirmDialog, setDeleteConfirmDialog] = useState<{
    open: boolean;
    varName: string;
  }>({ open: false, varName: "" });
  const [visibleFields, setVisibleFields] = useState<Set<string>>(new Set());

  useRegisterEnvironmentTelemetry({
    environmentSlug: environment?.slug ?? "",
  });

  const deleteEnvironmentMutation = useDeleteEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_deleted",
      });
      environment!.refetch();
      navigate("/environments");
    },
  });

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_updated",
      });
      environment!.refetch();
      setHasChanges(false);
      setSaveError(null);
      setEnvValues({});
      setEditedFields(new Set());
      setDeletedFields(new Set());
    },
    onError: (error) => {
      console.error(
        "Environment variable save failed:",
        error?.message || error,
      );
      setSaveError("Failed to save environment variables. Please try again.");
    },
  });

  const { mutate: updateEnvironment, isPending: isSaving } =
    updateEnvironmentMutation;

  // Reset state when environment changes (like ToolsetAuth)
  useEffect(() => {
    setEnvValues({});
    setEditedFields(new Set());
    setDeletedFields(new Set());
    setHasChanges(false);
    setSaveError(null);
    setFocusedField(null);
    setVisibleFields(new Set());
  }, [environment?.slug]);

  const { data: selectedToolset } = useToolset(
    { slug: selectedToolsetSlug },
    undefined,
    { enabled: !!selectedToolsetSlug },
  );

  useEffect(() => {
    if (
      selectedToolset &&
      (selectedToolset.securityVariables ||
        selectedToolset.serverVariables ||
        selectedToolset.functionEnvironmentVariables ||
        selectedToolset.externalMcpHeaderDefinitions)
    ) {
      const newValues = { ...envValues };
      const newEdited = new Set(editedFields);

      // Process security variables
      selectedToolset.securityVariables?.forEach((entry) => {
        entry.envVariables.forEach((varName) => {
          const existingEntry = environment?.entries?.find(
            (e) => e.name === varName,
          );
          if (!existingEntry) {
            newValues[varName] = "";
            newEdited.add(varName);
          }
        });
      });

      // Process function environment variables
      selectedToolset.functionEnvironmentVariables?.forEach((entry) => {
        const existingEntry = environment?.entries?.find(
          (e) => e.name === entry.name,
        );
        if (!existingEntry) {
          newValues[entry.name] = "";
          newEdited.add(entry.name);
        }
      });

      // Process server variables
      selectedToolset.serverVariables?.forEach((entry) => {
        entry.envVariables.forEach((varName) => {
          const existingEntry = environment?.entries?.find(
            (e) => e.name === varName,
          );
          if (!existingEntry) {
            newValues[varName] = "";
            newEdited.add(varName);
          }
        });
      });

      // Process external MCP header definitions
      selectedToolset.externalMcpHeaderDefinitions?.forEach((entry) => {
        const existingEntry = environment?.entries?.find(
          (e) => e.name === entry.name,
        );
        if (!existingEntry) {
          newValues[entry.name] = "";
          newEdited.add(entry.name);
        }
      });

      setEnvValues(newValues);
      setEditedFields(newEdited);
      setHasChanges(true);
      setSelectedToolsetSlug("");
    }
  }, [selectedToolset, environment?.entries, envValues, editedFields]);

  const handleValueChange = useCallback(
    (varName: string, value: string) => {
      setEnvValues((prev) => ({ ...prev, [varName]: value }));
      setEditedFields((prev) => new Set(prev).add(varName));
      setHasChanges(true);
      if (saveError) setSaveError(null);
    },
    [saveError],
  );

  const handleFieldFocus = useCallback((varName: string) => {
    setFocusedField(varName);
  }, []);

  const handleFieldBlur = useCallback(() => {
    setFocusedField(null);
  }, []);

  const handleToggleVisibility = useCallback((varName: string) => {
    setVisibleFields((prev) => {
      const next = new Set(prev);
      if (next.has(varName)) {
        next.delete(varName);
      } else {
        next.add(varName);
      }
      return next;
    });
  }, []);

  const handleCancel = useCallback(() => {
    setEnvValues({});
    setEditedFields(new Set());
    setDeletedFields(new Set());
    setHasChanges(false);
    setSaveError(null);
    setFocusedField(null);
    setVisibleFields(new Set());
    setIsAddingNew(false);
    setNewEntryName("");
    setNewEntryValue("");
  }, []);

  const handleRemoveVariable = useCallback((varName: string) => {
    setDeleteConfirmDialog({ open: true, varName });
  }, []);

  const confirmDelete = useCallback(
    (varName: string) => {
      setDeletedFields((prev) => new Set(prev).add(varName));
      setHasChanges(true);
      if (saveError) setSaveError(null);
      setDeleteConfirmDialog({ open: false, varName: "" });
    },
    [saveError],
  );

  const handleSave = useCallback(() => {
    if (!environment) return;

    const { slug: environmentSlug } = environment;

    // Include new entry if adding one (even with empty value)
    const allValues = { ...envValues };
    if (isAddingNew && validateEntryName(newEntryName)) {
      allValues[newEntryName.trim()] = newEntryValue.trim();
    }

    const entriesToUpdate = Object.entries(allValues)
      .filter(([name]) => !deletedFields.has(name))
      .map(([name, value]) => ({ name, value }));

    const entriesToRemove: string[] = Array.from(deletedFields);

    updateEnvironment({
      request: {
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate,
          entriesToRemove,
        },
      },
    });

    // Reset new entry state
    setIsAddingNew(false);
    setNewEntryName("");
    setNewEntryValue("");
    setNewEntryVisible(false);
    setDeletedFields(new Set());
  }, [
    environment,
    envValues,
    isAddingNew,
    newEntryName,
    newEntryValue,
    deletedFields,
    updateEnvironment,
  ]);

  const handleAddNewEntry = useCallback(() => {
    setIsAddingNew(true);
  }, []);

  const handleCancelNewEntry = useCallback(() => {
    setIsAddingNew(false);
    setNewEntryName("");
    setNewEntryValue("");
    setNewEntryVisible(false);
  }, []);

  const validateEntryName = (name: string) => {
    return (
      name.length > 0 &&
      !environment?.entries.some((entry) => entry.name === name) &&
      !Object.keys(envValues).includes(name) &&
      /^[-_.a-zA-Z][-_.a-zA-Z0-9]*$/.test(name)
    );
  };

  const handleToolsetSubmit = (toolsetSlug: string) => {
    setSelectedToolsetSlug(toolsetSlug);
  };

  if (!environment) {
    return <div>Environment not found</div>;
  }

  const allEntries = [
    ...(environment.entries || []),
    ...Object.keys(envValues)
      .filter((name) => !environment.entries?.find((e) => e.name === name))
      .map((name) => ({
        name,
        value: "",
        createdAt: new Date(),
        updatedAt: new Date(),
      })),
  ].filter((entry) => !deletedFields.has(entry.name));

  const hasChangesOrNewEntry =
    hasChanges || (isAddingNew && validateEntryName(newEntryName));

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>{environment.name}</Page.Section.Title>
          <Page.Section.CTA>
            <Button
              onClick={handleAddNewEntry}
              disabled={isSaving || isAddingNew}
            >
              Add Variable
            </Button>
          </Page.Section.CTA>
          <MoreActions
            actions={[
              {
                label: "Fill for Toolset",
                onClick: () => setToolsetDialogOpen(true),
                icon: "copy-plus",
              },
              {
                label: "Delete Environment",
                onClick: () =>
                  deleteEnvironmentMutation.mutate({
                    request: { slug: environment.slug },
                  }),
                icon: "trash",
                destructive: true,
              },
            ]}
          />
          <Page.Section.Body>
            <div className="space-y-6">
              <div className="space-y-4">
                {allEntries.map((entry) => {
                  const isEdited = editedFields.has(entry.name);
                  const originalEntry = environment.entries?.find(
                    (e) => e.name === entry.name,
                  );
                  const isNew = !originalEntry;
                  const isFocused = focusedField === entry.name;
                  const hasExistingValue =
                    originalEntry?.value != null &&
                    originalEntry.value.trim() !== "";

                  // Display logic matching ToolsetAuth:
                  // - If edited, show the edited value
                  // - If not focused and has existing value, show the original value
                  // - If focused, show empty (to allow typing replacement)
                  let displayValue = "";
                  if (isEdited) {
                    displayValue = envValues[entry.name] ?? "";
                  } else if (
                    !isFocused &&
                    hasExistingValue &&
                    originalEntry?.value
                  ) {
                    displayValue = originalEntry.value;
                  }

                  return (
                    <div
                      key={entry.name}
                      className="grid grid-cols-2 gap-4 items-center mb-2"
                    >
                      <label className="text-sm font-medium text-foreground">
                        {entry.name}
                        {isNew && (
                          <span className="ml-2 text-xs text-blue-600 font-normal">
                            (new)
                          </span>
                        )}
                      </label>
                      <div className="flex items-center gap-2 w-full">
                        <div className="flex-1">
                          <Input
                            value={displayValue}
                            onChange={(value) =>
                              handleValueChange(entry.name, value)
                            }
                            onFocus={() => handleFieldFocus(entry.name)}
                            onBlur={handleFieldBlur}
                            placeholder={
                              hasExistingValue
                                ? "Replace existing value"
                                : "Enter value"
                            }
                            type={
                              visibleFields.has(entry.name)
                                ? "text"
                                : "password"
                            }
                            className={`font-mono text-sm w-full ${isEdited ? "ring-1 ring-blue-500" : ""}`}
                            disabled={isSaving}
                          />
                        </div>
                        <Button
                          variant="tertiary"
                          size="sm"
                          className="h-8 w-8 flex-shrink-0 self-start mt-[1px]"
                          onClick={() => handleToggleVisibility(entry.name)}
                          disabled={isSaving}
                          aria-label={
                            visibleFields.has(entry.name)
                              ? `Hide ${entry.name}`
                              : `Show ${entry.name}`
                          }
                        >
                          <Button.LeftIcon>
                            {visibleFields.has(entry.name) ? (
                              <EyeOff className="h-4 w-4" />
                            ) : (
                              <Eye className="h-4 w-4" />
                            )}
                          </Button.LeftIcon>
                        </Button>
                        <Button
                          variant="tertiary"
                          size="sm"
                          className="h-8 w-8 flex-shrink-0 self-start mt-[1px]"
                          onClick={() => handleRemoveVariable(entry.name)}
                          disabled={isSaving}
                          aria-label={`Remove ${entry.name}`}
                        >
                          <Button.LeftIcon>
                            <Trash2 className="h-4 w-4" />
                          </Button.LeftIcon>
                        </Button>
                      </div>
                    </div>
                  );
                })}

                {isAddingNew && (
                  <div className="space-y-2">
                    <div className="grid grid-cols-2 gap-4 items-center mb-2">
                      <Input
                        value={newEntryName}
                        onChange={(value) =>
                          setNewEntryName(value.toUpperCase())
                        }
                        onKeyDown={(e) => {
                          if (e.key === "Escape") {
                            handleCancelNewEntry();
                          }
                        }}
                        placeholder="NAME"
                        className="font-mono text-sm w-full"
                        disabled={isSaving}
                        autoFocus
                      />
                      <div className="flex items-center gap-2 w-full">
                        <div className="flex-1">
                          <Input
                            value={newEntryValue}
                            onChange={setNewEntryValue}
                            placeholder="Value"
                            type={newEntryVisible ? "text" : "password"}
                            className="font-mono text-sm w-full"
                            disabled={isSaving}
                          />
                        </div>
                        <Button
                          variant="tertiary"
                          size="sm"
                          className="h-8 w-8 flex-shrink-0 self-start mt-[1px]"
                          onClick={() => setNewEntryVisible(!newEntryVisible)}
                          disabled={isSaving}
                          aria-label={
                            newEntryVisible ? "Hide value" : "Show value"
                          }
                        >
                          <Button.LeftIcon>
                            {newEntryVisible ? (
                              <EyeOff className="h-4 w-4" />
                            ) : (
                              <Eye className="h-4 w-4" />
                            )}
                          </Button.LeftIcon>
                        </Button>
                        <Button
                          variant="tertiary"
                          size="sm"
                          className="h-8 w-8 flex-shrink-0 self-start mt-[1px]"
                          onClick={handleCancelNewEntry}
                          disabled={isSaving}
                          aria-label="Cancel new entry"
                        >
                          <Button.LeftIcon>
                            <X className="h-4 w-4" />
                          </Button.LeftIcon>
                        </Button>
                      </div>
                    </div>
                    {!validateEntryName(newEntryName) &&
                      newEntryName.length > 0 && (
                        <p className="text-xs text-destructive">
                          Variable name must start with a letter, underscore,
                          dash, or period and contain only alphanumeric
                          characters, underscores, dashes, or periods
                        </p>
                      )}
                  </div>
                )}
              </div>

              {hasChangesOrNewEntry && (
                <SaveActionBar
                  saveError={saveError}
                  isSaving={isSaving}
                  onSave={handleSave}
                  onCancel={handleCancel}
                />
              )}

              {allEntries.length === 0 && !isAddingNew && (
                <div className="text-center py-8">
                  <p className="text-sm text-muted-foreground">
                    No environment variables defined
                  </p>
                  <div className="mt-4 flex flex-col gap-2 items-center">
                    <Button onClick={handleAddNewEntry}>
                      <Plus className="h-4 w-4 mr-2" />
                      ADD YOUR FIRST VARIABLE
                    </Button>
                    <Button
                      variant="secondary"
                      onClick={() => setToolsetDialogOpen(true)}
                    >
                      FILL FOR TOOLSET
                    </Button>
                  </div>
                </div>
              )}
            </div>

            <ToolsetDialog
              open={toolsetDialogOpen}
              onOpenChange={setToolsetDialogOpen}
              onSubmit={handleToolsetSubmit}
            />

            <Dialog
              open={deleteConfirmDialog.open}
              onOpenChange={(open) =>
                !open && setDeleteConfirmDialog({ open: false, varName: "" })
              }
            >
              <Dialog.Content>
                <Dialog.Header>
                  <Dialog.Title>Delete Environment Variable</Dialog.Title>
                  <Dialog.Description>
                    Are you sure you want to delete{" "}
                    <strong>{deleteConfirmDialog.varName}</strong>? This action
                    will be permanent once you save your changes.
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
