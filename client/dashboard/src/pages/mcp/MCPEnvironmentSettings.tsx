import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMissingRequiredEnvVars } from "@/hooks/useEnvironmentVariables";
import { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllGetMcpMetadata,
  invalidateAllListEnvironments,
  invalidateAllToolset,
  useCreateEnvironmentMutation,
  useGetMcpMetadata,
  useListEnvironments,
  useMcpMetadataSetMutation,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { AddVariableSheet } from "./AddVariableSheet";
import { EnvironmentSwitcher } from "./EnvironmentSwitcher";
import { EnvironmentVariableRow } from "./EnvironmentVariableRow";
import {
  EnvVarState,
  EnvironmentVariable,
  getAllEnvironments,
  getValueForEnvironment,
} from "./environmentVariableUtils";
import { useEnvironmentVariables } from "./useEnvironmentVariables";

export function MCPAuthenticationTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const session = useSession();

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];
  const { data: mcpMetadataData } = useGetMcpMetadata(
    {
      toolsetSlug: toolset.slug,
    },
    undefined,
    {
      throwOnError: false,
      retry: false,
    },
  );
  const mcpMetadata = mcpMetadataData?.metadata;
  const mcpAttachedEnvironmentSlug =
    environments.find((e) => e.id === mcpMetadata?.defaultEnvironmentId)
      ?.slug || null;

  // Load environment variables using custom hook
  const loadedEnvVars = useEnvironmentVariables(
    toolset,
    environments,
    mcpMetadata,
  );

  // State for the list of environment variables (managed locally for UI updates)
  const [envVars, setEnvVars] = useState<EnvironmentVariable[]>([]);
  const [isAddingNew, setIsAddingNew] = useState(false);
  const [selectedEnvironmentView, setSelectedEnvironmentView] =
    useState<string>(toolset.defaultEnvironmentSlug || "default");

  // Sync loaded variables with local state
  useEffect(() => {
    setEnvVars(loadedEnvVars);
  }, [loadedEnvVars]);

  // Clear editing state when environment view changes
  useEffect(() => {
    setEditingState(new Map());
  }, [selectedEnvironmentView]);

  useEffect(() => {
    if (mcpAttachedEnvironmentSlug) {
      setSelectedEnvironmentView(mcpAttachedEnvironmentSlug);
    }
  }, [mcpAttachedEnvironmentSlug]);

  // Get attached environment and its available variables
  const attachedEnvironment = mcpMetadata?.defaultEnvironmentId
    ? environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId)
    : environments.find((e) => e.slug === "default") || null;

  const environmentEntries = mcpMetadata?.environmentEntries || [];
  const availableEnvVarsFromAttached =
    attachedEnvironment?.entries
      .map((entry) => entry.name)
      .filter(
        (name) => !environmentEntries.some((e) => e.variableName === name),
      ) || [];

  // Track which variable's header name is being edited
  const [editingHeaderId, setEditingHeaderId] = useState<string | null>(null);

  // Track editing state for required variables (value and header display name)
  type EditingState = {
    value: string;
    headerDisplayName?: string;
  };
  const [editingState, setEditingState] = useState<Map<string, EditingState>>(
    new Map(),
  );

  // Create environment dialog state
  const [isCreateEnvDialogOpen, setIsCreateEnvDialogOpen] = useState(false);
  const [newEnvironmentName, setNewEnvironmentName] = useState("");

  // Update environment mutation
  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      invalidateAllListEnvironments(queryClient);
      telemetry.capture("environment_event", {
        action: "environment_variable_updated",
        toolset_slug: toolset.slug,
      });
    },
  });

  // Create environment mutation
  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: (data) => {
      invalidateAllListEnvironments(queryClient);
      setSelectedEnvironmentView(data.slug);
      setIsCreateEnvDialogOpen(false);
      setNewEnvironmentName("");
      toast.success(`Created environment "${data.name}"`);
      telemetry.capture("environment_event", {
        action: "environment_created",
        environment_slug: data.slug,
      });
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to create environment",
      );
    },
  });

  // Set toolset environment link mutation (for making an environment the default)
  const setMcpMetadataMutation = useMcpMetadataSetMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      invalidateAllGetMcpMetadata(queryClient);
      toast.success("MCP metadata updated");
      telemetry.capture("mcp_event", {
        action: "mcp_metadata_updated",
        toolset_slug: toolset.slug,
      });
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update default environment",
      );
    },
  });

  const handleCreateEnvironment = () => {
    if (!newEnvironmentName.trim()) {
      toast.error("Environment name is required");
      return;
    }

    if (!session.activeOrganizationId) {
      toast.error("Organization ID not found");
      return;
    }

    createEnvironmentMutation.mutate({
      request: {
        createEnvironmentForm: {
          name: newEnvironmentName.trim(),
          organizationId: session.activeOrganizationId,
          entries: [],
        },
      },
    });
  };

  const handleLoadFromEnvironment = (varKey: string) => {
    if (!attachedEnvironment) {
      toast.error(
        "No attached environment found. Please configure an environment first.",
      );
      return;
    }

    // Create environment entry for this variable
    const existingEntries = mcpMetadata?.environmentEntries || [];
    if (!existingEntries.some((e) => e.variableName === varKey)) {
      const newEntries = [
        ...existingEntries,
        {
          variableName: varKey,
          providedBy: "system",
        },
      ];

      setMcpMetadataMutation.mutate({
        request: {
          setMcpMetadataRequestBody: {
            toolsetSlug: toolset.slug,
            defaultEnvironmentId:
              mcpMetadata?.defaultEnvironmentId || attachedEnvironment.id,
            environmentEntries: newEntries,
            externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
            instructions: mcpMetadata?.instructions,
            logoAssetId: mcpMetadata?.logoAssetId,
          },
        },
      });
      toast.success(`Added ${varKey} from ${attachedEnvironment.name}`);
      setIsAddingNew(false);

      telemetry.capture("environment_event", {
        action: "environment_variable_loaded_from_environment",
        toolset_slug: toolset.slug,
      });
    } else {
      toast.error(`${varKey} is already attached`);
    }
  };

  const handleAddVariable = (
    varKey: string,
    newValue: string,
    newState: EnvVarState,
  ) => {
    if (newState === "system" && newValue) {
      updateEnvironmentMutation.mutate({
        request: {
          slug: selectedEnvironmentView,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [{ name: varKey, value: newValue }],
            entriesToRemove: [],
          },
        },
      });

      // Create environment entry for custom variables
      const existingEntries = mcpMetadata?.environmentEntries || [];
      if (!existingEntries.some((e) => e.variableName === varKey)) {
        const targetEnv = environments.find(
          (e) => e.slug === selectedEnvironmentView,
        );
        if (targetEnv) {
          const newEntries = [
            ...existingEntries,
            {
              variableName: varKey,
              providedBy: "system",
            },
          ];

          setMcpMetadataMutation.mutate({
            request: {
              setMcpMetadataRequestBody: {
                toolsetSlug: toolset.slug,
                defaultEnvironmentId:
                  mcpMetadata?.defaultEnvironmentId || targetEnv.id,
                environmentEntries: newEntries,
                externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
                instructions: mcpMetadata?.instructions,
                logoAssetId: mcpMetadata?.logoAssetId,
              },
            },
          });
        }
      }
    }

    telemetry.capture("environment_event", {
      action: "environment_variable_added",
      toolset_slug: toolset.slug,
      state: newState,
    });
  };

  const handleDeleteVariable = (id: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Delete from all environments that have this variable
    const allEnvs = getAllEnvironments(envVar);
    allEnvs.forEach((envSlug) => {
      updateEnvironmentMutation.mutate({
        request: {
          slug: envSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [],
            entriesToRemove: [envVar.key],
          },
        },
      });
    });

    // If this is a custom variable (not required), also remove its environment entry
    if (!envVar.isRequired) {
      const existingEntries = mcpMetadata?.environmentEntries || [];
      const updatedEntries = existingEntries.filter(
        (e) => e.variableName !== envVar.key,
      );

      if (updatedEntries.length !== existingEntries.length) {
        setMcpMetadataMutation.mutate({
          request: {
            setMcpMetadataRequestBody: {
              toolsetSlug: toolset.slug,
              defaultEnvironmentId: mcpMetadata?.defaultEnvironmentId,
              environmentEntries: updatedEntries,
              externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
              instructions: mcpMetadata?.instructions,
              logoAssetId: mcpMetadata?.logoAssetId,
            },
          },
        });
      }
    }

    telemetry.capture("environment_event", {
      action: "environment_variable_deleted",
      toolset_slug: toolset.slug,
    });
  };

  // Separate required and custom variables, sort omitted to the bottom
  const requiredVars = envVars
    .filter((v) => v.isRequired)
    .sort((a, b) => {
      // Sort omitted vars to the bottom
      if (a.state === "omitted" && b.state !== "omitted") return 1;
      if (a.state !== "omitted" && b.state === "omitted") return -1;
      return 0;
    });

  // Count missing required variables (user-provided ones count as configured)
  const missingRequiredCount = useMissingRequiredEnvVars(
    toolset,
    environments,
    selectedEnvironmentView,
    mcpMetadata,
  );

  // Handle value change for variables
  const handleValueChange = (id: string, newValue: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // If value is empty, clear editing state to reflect current state
    if (!newValue) {
      const newEditingState = new Map(editingState);
      newEditingState.delete(id);
      setEditingState(newEditingState);
      return;
    }

    setEditingState(new Map(editingState.set(id, { value: newValue })));
  };

  // Get editing value for a variable (either from editing state or from valueGroups)
  const getEditingValue = (envVar: EnvironmentVariable): string => {
    if (editingState.has(envVar.id)) {
      return editingState.get(envVar.id)!.value;
    }
    // Show the value for the currently selected environment
    return getValueForEnvironment(envVar, selectedEnvironmentView);
  };

  // Get header display name for a variable
  const getHeaderDisplayName = (envVar: EnvironmentVariable): string => {
    if (
      editingState.has(envVar.id) &&
      editingState.get(envVar.id)!.headerDisplayName !== undefined
    ) {
      return editingState.get(envVar.id)!.headerDisplayName!;
    }
    const entry = environmentEntries.find((e) => e.variableName === envVar.key);
    return entry?.headerDisplayName || "";
  };

  // Handle header display name change
  const handleHeaderDisplayNameChange = (id: string, newName: string) => {
    const current = editingState.get(id);
    if (current) {
      setEditingState(
        new Map(
          editingState.set(id, { ...current, headerDisplayName: newName }),
        ),
      );
    } else {
      // Initialize editing state with current values
      const envVar = envVars.find((v) => v.id === id);
      if (!envVar) return;

      const value = getEditingValue(envVar);

      setEditingState(
        new Map(
          editingState.set(id, {
            value,
            headerDisplayName: newName,
          }),
        ),
      );
    }
  };

  // Check if a variable has unsaved changes or has no environment entry (unmapped)
  const hasUnsavedChanges = (envVar: EnvironmentVariable): boolean => {
    // Find existing environment entry
    const entry = environmentEntries.find((e) => e.variableName === envVar.key);

    // If no entry exists, this is an unmapped required variable that needs to be saved
    if (!entry && envVar.isRequired) {
      return true;
    }

    // Determine the original state based on environment entry
    const originalState: EnvVarState =
      entry?.providedBy === "user"
        ? "user-provided"
        : entry?.providedBy === "none"
          ? "omitted"
          : "system";

    // Check if state has changed
    if (envVar.state !== originalState) {
      return true;
    }

    // If state is system, check if value changed
    if (envVar.state === "system" && editingState.has(envVar.id)) {
      const editing = editingState.get(envVar.id)!;
      const currentValue = getValueForEnvironment(
        envVar,
        selectedEnvironmentView,
      );

      // Check if value changed (only if a value is provided)
      if (editing.value && editing.value !== currentValue) {
        return true;
      }
    }

    // Check if header display name changed
    if (editingState.has(envVar.id)) {
      const editing = editingState.get(envVar.id)!;
      const originalHeaderName = entry?.headerDisplayName || "";
      if (
        editing.headerDisplayName !== undefined &&
        editing.headerDisplayName !== originalHeaderName
      ) {
        return true;
      }
    }

    return false;
  };

  // Check if there are any unsaved changes across all variables
  const hasAnyUnsavedChanges = useMemo(() => {
    return envVars.some(hasUnsavedChanges);
  }, [envVars, editingState, environmentEntries, selectedEnvironmentView]);

  // Save all variables with unsaved changes
  const handleSaveAll = async () => {
    const varsToSave = envVars.filter(hasUnsavedChanges);
    for (const envVar of varsToSave) {
      await handleSaveVariable(envVar);
    }
  };

  // Cancel all changes and reset editing state
  const handleCancelAll = () => {
    setEditingState(new Map());
    // Reset state changes by reloading from hook
    setEnvVars(loadedEnvVars);
  };

  // Cycle between user-provided, system, and omitted states
  const handleToggleState = (id: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    const nextState: EnvVarState =
      envVar.state === "user-provided"
        ? "system"
        : envVar.state === "system"
          ? "omitted"
          : "user-provided";

    // Update local state
    setEnvVars(
      envVars.map((v) => {
        if (v.id !== id) return v;
        return { ...v, state: nextState };
      }),
    );

    // Initialize or update editing state to track the state change
    const newEditingState = new Map(editingState);
    const currentValue = getValueForEnvironment(
      envVar,
      selectedEnvironmentView,
    );
    const currentHeaderName = getHeaderDisplayName(envVar);

    newEditingState.set(id, {
      value: currentValue,
      headerDisplayName: currentHeaderName,
    });

    setEditingState(newEditingState);
  };

  // Save a required variable
  const handleSaveVariable = async (envVar: EnvironmentVariable) => {
    const existingEntries = mcpMetadata?.environmentEntries || [];
    const currentDefaultEnvId = mcpMetadata?.defaultEnvironmentId;

    // Get editing state
    const editing = editingState.get(envVar.id);

    // Only update headerDisplayName if it's explicitly being edited
    const isHeaderNameBeingEdited = editing?.headerDisplayName !== undefined;
    const newHeaderName = isHeaderNameBeingEdited
      ? editing.headerDisplayName
      : undefined;

    // Handle user-provided and omitted states (no value needed)
    if (envVar.state === "user-provided" || envVar.state === "omitted") {
      // Create or update environment entry
      const entryIndex = existingEntries.findIndex(
        (e) => e.variableName === envVar.key,
      );
      let updatedEntries;

      const providedByValue =
        envVar.state === "user-provided" ? "user" : "none";

      if (entryIndex >= 0) {
        // Update existing entry - preserve headerDisplayName unless explicitly changed
        updatedEntries = [...existingEntries];
        updatedEntries[entryIndex] = {
          ...existingEntries[entryIndex],
          variableName: envVar.key,
          providedBy: providedByValue,
          // Only update headerDisplayName if it's explicitly being edited
          ...(isHeaderNameBeingEdited
            ? { headerDisplayName: newHeaderName || undefined }
            : {}),
        };
      } else {
        // Create new entry
        updatedEntries = [
          ...existingEntries,
          {
            variableName: envVar.key,
            providedBy: providedByValue,
            ...(isHeaderNameBeingEdited
              ? { headerDisplayName: newHeaderName || undefined }
              : {}),
          },
        ];
      }

      setMcpMetadataMutation.mutate({
        request: {
          setMcpMetadataRequestBody: {
            toolsetSlug: toolset.slug,
            defaultEnvironmentId: currentDefaultEnvId,
            environmentEntries: updatedEntries,
            externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
            instructions: mcpMetadata?.instructions,
            logoAssetId: mcpMetadata?.logoAssetId,
          },
        },
      });

      // Clear editing state after save
      const newEditingState = new Map(editingState);
      newEditingState.delete(envVar.id);
      setEditingState(newEditingState);

      const actionMessage =
        envVar.state === "user-provided"
          ? "set to user-provided"
          : "set to omitted";
      toast.success(`${envVar.key} ${actionMessage}`);
      telemetry.capture("environment_event", {
        action:
          envVar.state === "user-provided"
            ? "variable_set_user_provided"
            : "variable_set_omitted",
        toolset_slug: toolset.slug,
        variable_key: envVar.key,
      });
      return;
    }

    // Handle system state
    const value = getEditingValue(envVar);

    // Save to the currently selected environment
    const targetEnv = environments.find(
      (e) => e.slug === selectedEnvironmentView,
    );

    if (!targetEnv) {
      toast.error("Target environment not found");
      return;
    }

    // Update environment variable only if there's a value
    if (value) {
      updateEnvironmentMutation.mutate({
        request: {
          slug: selectedEnvironmentView,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [{ name: envVar.key, value }],
            entriesToRemove: [],
          },
        },
      });
    }

    // Create or update environment entry
    const entryIndex = existingEntries.findIndex(
      (e) => e.variableName === envVar.key,
    );
    let updatedEntries;

    if (entryIndex >= 0) {
      // Update existing entry - preserve headerDisplayName unless explicitly changed
      updatedEntries = [...existingEntries];
      updatedEntries[entryIndex] = {
        ...existingEntries[entryIndex],
        variableName: envVar.key,
        providedBy: "system",
        // Only update headerDisplayName if it's explicitly being edited
        ...(isHeaderNameBeingEdited
          ? { headerDisplayName: newHeaderName || undefined }
          : {}),
      };
    } else {
      // Create new entry
      updatedEntries = [
        ...existingEntries,
        {
          variableName: envVar.key,
          providedBy: "system",
          ...(isHeaderNameBeingEdited
            ? { headerDisplayName: newHeaderName || undefined }
            : {}),
        },
      ];
    }

    setMcpMetadataMutation.mutate({
      request: {
        setMcpMetadataRequestBody: {
          toolsetSlug: toolset.slug,
          defaultEnvironmentId: currentDefaultEnvId || targetEnv.id,
          environmentEntries: updatedEntries,
          externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
          instructions: mcpMetadata?.instructions,
          logoAssetId: mcpMetadata?.logoAssetId,
        },
      },
    });

    // Clear editing state after save
    const newEditingState = new Map(editingState);
    newEditingState.delete(envVar.id);
    setEditingState(newEditingState);

    if (value) {
      toast.success(`Saved ${envVar.key} to ${targetEnv.name}`);
    } else {
      toast.success(`Updated state for ${envVar.key}`);
    }

    telemetry.capture("environment_event", {
      action: "required_variable_configured",
      toolset_slug: toolset.slug,
      variable_key: envVar.key,
    });
  };

  const handleSetDefaultEnvironment = () => {
    const targetEnv = environments.find(
      (e) => e.slug === selectedEnvironmentView,
    );
    if (!targetEnv) return;

    // Set this environment as the default
    setMcpMetadataMutation.mutate({
      request: {
        setMcpMetadataRequestBody: {
          toolsetSlug: toolset.slug,
          defaultEnvironmentId: targetEnv.id,
          environmentEntries: mcpMetadata?.environmentEntries || [],
          externalDocumentationUrl: mcpMetadata?.externalDocumentationUrl,
          instructions: mcpMetadata?.instructions,
          logoAssetId: mcpMetadata?.logoAssetId,
        },
      },
    });

    toast.success(`Set ${targetEnv.name} as default environment`);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-semibold tracking-tight">
            Environment Variables
          </h2>
          {missingRequiredCount > 0 && (
            <Badge variant="warning">
              <Badge.LeftIcon>
                <AlertTriangle className="h-3.5 w-3.5" />
              </Badge.LeftIcon>
              <Badge.Text>
                {missingRequiredCount} required not configured
              </Badge.Text>
            </Badge>
          )}
        </div>
        <Button onClick={() => setIsAddingNew(true)} disabled={isAddingNew}>
          <Button.Text>Add Variable</Button.Text>
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Configure required credentials and add custom variables. Click the state
        button to cycle between User Provided (set at runtime), System (set
        here), and Omitted (not included).
      </p>

      {/* All Variables Section */}
      <div className="space-y-4">
        {/* Variables List */}
        {envVars.length > 0 ? (
          <div className="border rounded-lg overflow-hidden">
            {/* Environment Switcher Tabs */}
            <EnvironmentSwitcher
              environments={environments}
              selectedEnvironmentView={selectedEnvironmentView}
              mcpAttachedEnvironmentSlug={mcpAttachedEnvironmentSlug}
              defaultEnvironmentSlug={
                toolset.defaultEnvironmentSlug || "default"
              }
              requiredVars={requiredVars}
              hasAnyUnsavedChanges={hasAnyUnsavedChanges}
              onEnvironmentSelect={setSelectedEnvironmentView}
              onSaveAll={handleSaveAll}
              onCancelAll={handleCancelAll}
              onSetDefaultEnvironment={handleSetDefaultEnvironment}
              onCreateEnvironment={() => setIsCreateEnvDialogOpen(true)}
            />
            {envVars.map((envVar, index) => (
              <EnvironmentVariableRow
                key={envVar.id}
                envVar={envVar}
                index={index}
                totalCount={envVars.length}
                selectedEnvironmentView={selectedEnvironmentView}
                mcpAttachedEnvironmentSlug={mcpAttachedEnvironmentSlug}
                defaultEnvironmentSlug={
                  toolset.defaultEnvironmentSlug || "default"
                }
                environmentEntries={environmentEntries}
                editingState={editingState}
                editingHeaderId={editingHeaderId}
                onToggleState={handleToggleState}
                onValueChange={handleValueChange}
                onDelete={handleDeleteVariable}
                onEditHeaderName={setEditingHeaderId}
                onHeaderDisplayNameChange={handleHeaderDisplayNameChange}
                onHeaderBlur={() => setEditingHeaderId(null)}
              />
            ))}
          </div>
        ) : (
          // Empty State
          <div className="border rounded-lg border-dashed p-8 text-center">
            <p className="text-muted-foreground mb-4">
              No environment variables added yet.
            </p>
            <Button onClick={() => setIsAddingNew(true)} variant="secondary">
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add Variable</Button.Text>
            </Button>
          </div>
        )}
      </div>

      {/* Add New Variable Sheet */}
      <AddVariableSheet
        open={isAddingNew}
        onOpenChange={setIsAddingNew}
        attachedEnvironment={attachedEnvironment}
        availableEnvVarsFromAttached={availableEnvVarsFromAttached}
        onAddVariable={handleAddVariable}
        onLoadFromEnvironment={handleLoadFromEnvironment}
      />

      {/* Create Environment Dialog */}
      <Dialog
        open={isCreateEnvDialogOpen}
        onOpenChange={setIsCreateEnvDialogOpen}
      >
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Create New Environment</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <div>
              <Label className="text-sm font-medium mb-2 block">
                Environment Name
              </Label>
              <Input
                value={newEnvironmentName}
                onChange={setNewEnvironmentName}
                placeholder="staging, production, dev..."
                autoFocus
                onKeyDown={(e: React.KeyboardEvent) => {
                  if (e.key === "Enter") {
                    handleCreateEnvironment();
                  }
                }}
              />
            </div>
          </div>
          <Dialog.Footer className="flex justify-end gap-2">
            <Button
              variant="tertiary"
              onClick={() => {
                setIsCreateEnvDialogOpen(false);
                setNewEnvironmentName("");
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={handleCreateEnvironment}
              disabled={
                !newEnvironmentName.trim() ||
                createEnvironmentMutation.isPending
              }
            >
              {createEnvironmentMutation.isPending ? "Creating..." : "Create"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
