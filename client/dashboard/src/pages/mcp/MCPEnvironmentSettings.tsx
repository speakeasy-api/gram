import { EnvironmentVariableDialog } from "@/components/environments/EnvironmentVariableDialog";
import { useExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG } from "@/lib/externalMcpUserSessions";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import type { McpEnvironmentConfigInput } from "@gram/client/models/components/mcpenvironmentconfiginput.js";
import { useCreateEnvironmentMutation } from "@gram/client/react-query/createEnvironment.js";
import {
  invalidateAllGetMcpMetadata,
  useGetMcpMetadata,
} from "@gram/client/react-query/getMcpMetadata.js";
import {
  invalidateAllListEnvironments,
  useListEnvironments,
} from "@gram/client/react-query/listEnvironments.js";
import { useMcpMetadataSetMutation } from "@gram/client/react-query/mcpMetadataSet.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateEnvironmentMutation } from "@gram/client/react-query/updateEnvironment.js";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle, Link, Plus, Shield } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { AddVariableSheet } from "./AddVariableSheet";
import { EnvironmentSwitcher } from "./EnvironmentSwitcher";
import { EnvironmentVariableRow } from "./EnvironmentVariableRow";
import {
  ConnectOAuthModal,
  OAuthDetailsModal,
  PageSection,
} from "./MCPDetails";
import {
  EnvVarState,
  EnvironmentVariable,
  getValueForEnvironment,
  hasEntryInEnvironment,
  isSecretInEnvironment,
} from "./environmentVariableUtils";
import {
  ConvertToUserSessionsButton,
  ToolsetAuthenticationSection,
} from "./ToolsetAuthenticationSection";
import {
  getOAuthParadigm,
  isUserSessionIssuerWired,
  type OAuthParadigm,
  toolsetAuthSurface,
  type ToolsetConvertAction,
  toolsetConvertAction,
} from "./toolsetAuthSurface";
import { useEnvironmentVariables } from "./useEnvironmentVariables";

// Empty array constant to avoid creating new references
const EMPTY_ENVIRONMENTS: never[] = [];

export function MCPAuthenticationTab({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const session = useSession();
  const routes = useRoutes();

  const { data: environmentsData } = useListEnvironments();
  // Use stable reference for empty array to prevent infinite loops
  const environments = environmentsData?.environments ?? EMPTY_ENVIRONMENTS;
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
  const mcpAttachedEnvironmentSlug = useMemo(
    () =>
      environments.find((e) => e.id === mcpMetadata?.defaultEnvironmentId)
        ?.slug || null,
    [environments, mcpMetadata?.defaultEnvironmentId],
  );

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

  // Track if we've initialized from loaded vars to prevent re-syncing on every change
  const hasInitialized = useRef(false);
  const prevLoadedVarsRef = useRef<EnvironmentVariable[]>([]);

  // Sync loaded variables with local state only when they actually change
  useEffect(() => {
    // Skip if no vars loaded yet
    if (loadedEnvVars.length === 0 && !hasInitialized.current) {
      return;
    }

    // Create a hash that includes key, state, and per-environment values
    const createHash = (vars: EnvironmentVariable[]) =>
      vars
        .map((v) => {
          const valuesHash = v.environmentValues
            .map((ev) => `${ev.environmentSlug}=${ev.value}:${ev.isSecret}`)
            .sort()
            .join("|");
          return `${v.key}:${v.state}:${valuesHash}`;
        })
        .join(",");

    const prevHash = createHash(prevLoadedVarsRef.current);
    const newHash = createHash(loadedEnvVars);

    if (prevHash !== newHash || !hasInitialized.current) {
      setEnvVars(loadedEnvVars);
      prevLoadedVarsRef.current = loadedEnvVars;
      hasInitialized.current = true;
    }
  }, [loadedEnvVars]);

  // Clear editing state when environment view changes
  useEffect(() => {
    setEditingState(new Map());
  }, [selectedEnvironmentView]);

  // Track previous mcpAttachedEnvironmentSlug to only update when it actually changes
  const prevAttachedSlugRef = useRef<string | null>(null);
  useEffect(() => {
    if (
      mcpAttachedEnvironmentSlug &&
      mcpAttachedEnvironmentSlug !== prevAttachedSlugRef.current
    ) {
      prevAttachedSlugRef.current = mcpAttachedEnvironmentSlug;
      setSelectedEnvironmentView(mcpAttachedEnvironmentSlug);
    }
  }, [mcpAttachedEnvironmentSlug]);

  // Get attached environment and its available variables — only if explicitly set
  const attachedEnvironment = mcpMetadata?.defaultEnvironmentId
    ? (environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId) ??
      null)
    : null;

  const environmentConfigs = mcpMetadata?.environmentConfigs || [];
  const availableEnvVarsFromAttached =
    attachedEnvironment?.entries
      .map((entry) => entry.name)
      .filter(
        (name) => !environmentConfigs.some((e) => e.variableName === name),
      ) || [];

  // Track which variable's header name is being edited
  const [editingHeaderId, setEditingHeaderId] = useState<string | null>(null);

  // The variable whose value is open in the edit dialog, if any. The dialog
  // saves to the environment itself, so this never joins the editing state the
  // Save button drains. The environment slug is captured at click time: the
  // selected view can change under an open dialog (the attached-environment
  // effect moves it on a metadata refetch), and the save must hit the
  // environment the user saw.
  const [editingValueVar, setEditingValueVar] = useState<{
    envVar: EnvironmentVariable;
    environmentSlug: string;
  } | null>(null);

  // The variable whose stored value is pending delete confirmation. The slug
  // is captured at click time for the same reason as above.
  const [deletingValueVar, setDeletingValueVar] = useState<{
    name: string;
    environmentSlug: string;
  } | null>(null);

  // Track editing state for required variables. Values are saved by the
  // dialog as soon as it is submitted, so only the header display name, which
  // lives in MCP metadata alongside the mode, is staged for the Save button.
  type EditingState = {
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
      void invalidateAllListEnvironments(queryClient);
      telemetry.capture("environment_event", {
        action: "environment_variable_updated",
        toolset_slug: toolset.slug,
      });
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update environment variables",
      );
    },
  });

  // Create environment mutation
  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: (data) => {
      void invalidateAllListEnvironments(queryClient);
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
      // Note: handleSaveAll uses mutateAsync and handles invalidation itself
      // This onSuccess is for other callers like handleSetDefaultEnvironment
      void invalidateAllToolset(queryClient);
      void invalidateAllGetMcpMetadata(queryClient);
      telemetry.capture("mcp_event", {
        action: "mcp_metadata_updated",
        toolset_slug: toolset.slug,
      });
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update attached environment",
      );
    },
  });

  const handleDetachEnvironment = () => {
    setMcpMetadataMutation.mutate(
      {
        request: {
          setMcpMetadataRequestBody: {
            ...mcpMetadata,
            toolsetSlug: toolset.slug,
            // Omitting defaultEnvironmentId causes the upsert to write NULL, detaching the environment
            defaultEnvironmentId: undefined,
            environmentConfigs: mcpMetadata?.environmentConfigs || [],
          },
        },
      },
      {
        onSuccess: () => {
          toast.success("Detached environment from this MCP server");
        },
      },
    );
  };

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
    const existingEntries = mcpMetadata?.environmentConfigs || [];
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
            ...mcpMetadata,
            toolsetSlug: toolset.slug,
            defaultEnvironmentId:
              mcpMetadata?.defaultEnvironmentId || attachedEnvironment.id,
            environmentConfigs: newEntries,
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

  const handleAddVariables = (
    entries: Array<{
      key: string;
      value: string;
      state: EnvVarState;
      isSecret: boolean;
    }>,
  ) => {
    // Deduplicate by key, keeping the last entry for each key
    const deduped = Array.from(
      new Map(entries.map((e) => [e.key, e])).values(),
    );
    const systemEntries = deduped.filter(
      (e) => e.state === "system" && e.value,
    );

    if (systemEntries.length > 0) {
      updateEnvironmentMutation.mutate({
        request: {
          slug: selectedEnvironmentView,
          updateEnvironmentRequestBody: {
            entriesToUpdate: systemEntries.map((e) => ({
              name: e.key,
              value: e.value,
              isSecret: e.isSecret,
            })),
            entriesToRemove: [],
          },
        },
      });

      // Create environment entries for custom variables
      const existingEntries = mcpMetadata?.environmentConfigs || [];
      const newConfigEntries = systemEntries
        .filter(
          (e) =>
            !existingEntries.some(
              (existing) => existing.variableName === e.key,
            ),
        )
        .map((e) => ({ variableName: e.key, providedBy: "system" }));

      if (newConfigEntries.length > 0) {
        const targetEnv = environments.find(
          (e) => e.slug === selectedEnvironmentView,
        );
        if (targetEnv) {
          setMcpMetadataMutation.mutate({
            request: {
              setMcpMetadataRequestBody: {
                ...mcpMetadata,
                toolsetSlug: toolset.slug,
                defaultEnvironmentId:
                  mcpMetadata?.defaultEnvironmentId || targetEnv.id,
                environmentConfigs: [...existingEntries, ...newConfigEntries],
              },
            },
          });
        }
      }
    }

    for (const entry of entries) {
      telemetry.capture("environment_event", {
        action: "environment_variable_added",
        toolset_slug: toolset.slug,
        state: entry.state,
      });
    }
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

  // Get header display name for a variable
  const getHeaderDisplayName = (envVar: EnvironmentVariable): string => {
    if (
      editingState.has(envVar.id) &&
      editingState.get(envVar.id)!.headerDisplayName !== undefined
    ) {
      return editingState.get(envVar.id)!.headerDisplayName!;
    }
    const entry = environmentConfigs.find((e) => e.variableName === envVar.key);
    return entry?.headerDisplayName || "";
  };

  // Handle header display name change
  const handleHeaderDisplayNameChange = (id: string, newName: string) => {
    const current = editingState.get(id);
    const newEditingState = new Map(editingState);
    newEditingState.set(id, { ...current, headerDisplayName: newName });
    setEditingState(newEditingState);
  };

  // Check if a variable has actual user edits
  const hasUserEdits = (envVar: EnvironmentVariable): boolean => {
    const entry = environmentConfigs.find((e) => e.variableName === envVar.key);

    // Determine the original state based on environment entry
    const originalState: EnvVarState =
      entry?.providedBy === "user"
        ? "user-provided"
        : entry?.providedBy === "none"
          ? "omitted"
          : "system";

    // Check if state has changed (only if there was an existing entry)
    if (entry && envVar.state !== originalState) {
      return true;
    }

    // Check if the header display name changed
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

  // Check if a variable needs to be saved (includes unmapped required vars)
  const hasUnsavedChanges = (envVar: EnvironmentVariable): boolean => {
    // Check for actual user edits first
    if (hasUserEdits(envVar)) {
      return true;
    }

    // Also check if required variable has no entry (needs initial config)
    const entry = environmentConfigs.find((e) => e.variableName === envVar.key);
    if (!entry && envVar.isRequired) {
      return true;
    }

    return false;
  };

  // Check if there are any unsaved changes (including unmapped required vars and user edits)
  const hasAnyUnsavedChanges = useMemo(
    () => {
      return envVars.some(hasUnsavedChanges);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- hasUnsavedChanges is an inline fn; its reactive deps are listed explicitly
    [envVars, editingState, environmentConfigs, selectedEnvironmentView],
  );

  // Save all variables with unsaved changes
  const handleSaveAll = async () => {
    const varsToSave = envVars.filter(hasUnsavedChanges);
    if (varsToSave.length === 0) return;

    const existingEntries = mcpMetadata?.environmentConfigs || [];
    // Convert existing entries to input format
    const updatedEntriesMap = new Map<string, McpEnvironmentConfigInput>(
      existingEntries.map((e) => [
        e.variableName,
        {
          variableName: e.variableName,
          providedBy: e.providedBy,
          headerDisplayName: e.headerDisplayName,
        },
      ]),
    );
    // Process all variables and collect updates
    for (const envVar of varsToSave) {
      const editing = editingState.get(envVar.id);
      const isHeaderNameBeingEdited = editing?.headerDisplayName !== undefined;
      const newHeaderName = isHeaderNameBeingEdited
        ? editing.headerDisplayName
        : undefined;

      const providedByValue =
        envVar.state === "user-provided"
          ? "user"
          : envVar.state === "omitted"
            ? "none"
            : "system";

      // Update or create entry
      const existingEntry = updatedEntriesMap.get(envVar.key);
      updatedEntriesMap.set(envVar.key, {
        variableName: envVar.key,
        providedBy: providedByValue,
        headerDisplayName: isHeaderNameBeingEdited
          ? newHeaderName
          : existingEntry?.headerDisplayName,
      });
    }

    // Get target environment
    const targetEnv = environments.find(
      (e) => e.slug === selectedEnvironmentView,
    );

    if (!targetEnv) {
      toast.error("Target environment not found");
      return;
    }

    try {
      // Update MCP metadata with all environment entries
      const environmentConfigsToSave = Array.from(updatedEntriesMap.values());
      await setMcpMetadataMutation.mutateAsync({
        request: {
          setMcpMetadataRequestBody: {
            ...mcpMetadata,
            toolsetSlug: toolset.slug,
            defaultEnvironmentId: mcpMetadata?.defaultEnvironmentId,
            environmentConfigs: environmentConfigsToSave,
          },
        },
      });

      // Both mutations succeeded - invalidate and refetch
      await Promise.all([
        invalidateAllToolset(queryClient),
        invalidateAllGetMcpMetadata(queryClient),
        invalidateAllListEnvironments(queryClient),
      ]);

      // Clear editing state after data is refetched
      setEditingState(new Map());

      toast.success(`Saved ${varsToSave.length} variable(s)`);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to save variables",
      );
      return;
    }

    telemetry.capture("environment_event", {
      action: "bulk_save_variables",
      toolset_slug: toolset.slug,
      variable_count: varsToSave.length,
    });
  };

  // Cancel all changes and reset editing state
  const handleCancelAll = () => {
    setEditingState(new Map());
    // Reset state changes by reloading from hook
    setEnvVars(loadedEnvVars);
  };

  // Handle state change from dropdown
  const handleStateChange = (id: string, newState: EnvVarState) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Update local state
    setEnvVars(
      envVars.map((v) => {
        if (v.id !== id) return v;
        return { ...v, state: newState };
      }),
    );

    // Initialize or update editing state to track the state change
    const newEditingState = new Map(editingState);
    newEditingState.set(id, {
      headerDisplayName: getHeaderDisplayName(envVar),
    });

    setEditingState(newEditingState);
  };

  // Deleting the stored value is how a variable goes back to unset. A variable
  // the toolset advertises keeps its row and reads "Not set"; a custom one is
  // only listed because it has a stored value, so its row goes away with it.
  // A secret's value is unrecoverable once deleted, so it confirms first, like
  // the environment page.
  const confirmDeleteValue = (target: {
    name: string;
    environmentSlug: string;
  }) => {
    updateEnvironmentMutation.mutate({
      request: {
        slug: target.environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate: [],
          entriesToRemove: [target.name],
        },
      },
    });
    setDeletingValueVar(null);
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
          ...mcpMetadata,
          toolsetSlug: toolset.slug,
          defaultEnvironmentId: targetEnv.id,
          environmentConfigs: mcpMetadata?.environmentConfigs || [],
        },
      },
    });

    toast.success(`Set ${targetEnv.name} as attached environment`);
  };

  return (
    <Stack className="mb-4">
      <OAuthSection toolset={toolset} />

      <PageSection
        heading="Environment Variables"
        description="Environments store key-value pairs passed to the backend when users connect. They can be shared across multiple MCP servers. Use the state button to set each variable as User Provided (set at runtime), System (set here), or Omitted (not included)."
        headingExtra={
          missingRequiredCount > 0 ? (
            <Badge variant="warning" className="ml-2">
              <Badge.LeftIcon>
                <AlertTriangle className="h-3.5 w-3.5" />
              </Badge.LeftIcon>
              <Badge.Text>
                {missingRequiredCount} required not configured
              </Badge.Text>
            </Badge>
          ) : null
        }
        action={
          <Button onClick={() => setIsAddingNew(true)} disabled={isAddingNew}>
            <Button.Text>Add Variable</Button.Text>
          </Button>
        }
      >
        <div>
          {attachedEnvironment ? (
            <Badge variant="information">
              <Badge.LeftIcon>
                <Link className="h-3.5 w-3.5" />
              </Badge.LeftIcon>
              <Badge.Text>
                Attached:{" "}
                {attachedEnvironment.slug === "default"
                  ? "Default"
                  : attachedEnvironment.name}
              </Badge.Text>
            </Badge>
          ) : (
            <Badge variant="neutral">
              <Badge.Text>No environment attached</Badge.Text>
            </Badge>
          )}
        </div>

        <div className="space-y-4">
          {envVars.length > 0 ? (
            <div className="overflow-hidden rounded-lg border">
              <EnvironmentSwitcher
                environments={environments}
                selectedEnvironmentView={selectedEnvironmentView}
                mcpAttachedEnvironmentSlug={mcpAttachedEnvironmentSlug}
                defaultEnvironmentSlug={
                  toolset.defaultEnvironmentSlug || "default"
                }
                requiredVars={requiredVars}
                hasAnyUserEdits={hasAnyUnsavedChanges}
                hasExistingConfigs={environmentConfigs.length > 0}
                onEnvironmentSelect={setSelectedEnvironmentView}
                onSaveAll={() => void handleSaveAll()}
                onCancelAll={handleCancelAll}
                onSetDefaultEnvironment={handleSetDefaultEnvironment}
                onDetachEnvironment={handleDetachEnvironment}
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
                  environmentConfigs={environmentConfigs}
                  editingState={editingState}
                  editingHeaderId={editingHeaderId}
                  hasUnsavedChanges={hasUnsavedChanges(envVar)}
                  onStateChange={handleStateChange}
                  onEditValue={(v) =>
                    setEditingValueVar({
                      envVar: v,
                      environmentSlug: selectedEnvironmentView,
                    })
                  }
                  onDeleteValue={(v) =>
                    setDeletingValueVar({
                      name: v.key,
                      environmentSlug: selectedEnvironmentView,
                    })
                  }
                  onEditHeaderName={setEditingHeaderId}
                  onHeaderDisplayNameChange={handleHeaderDisplayNameChange}
                  onHeaderBlur={() => setEditingHeaderId(null)}
                />
              ))}
            </div>
          ) : (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <p className="text-muted-foreground mb-2">
                No environment variables configured yet.
              </p>
              <p className="text-muted-foreground mb-4 text-sm">
                Add key-value pairs to pass API keys, configuration, or any
                custom data to your backend. Environments can be shared across
                multiple MCP servers.
              </p>
              <Button onClick={() => setIsAddingNew(true)} variant="secondary">
                <Button.LeftIcon>
                  <Plus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Variable</Button.Text>
              </Button>
            </div>
          )}

          <div className="flex justify-end">
            <routes.environments.Link className="text-muted-foreground hover:text-foreground text-sm transition-colors">
              Manage environments →
            </routes.environments.Link>
          </div>
        </div>
      </PageSection>

      {/* Edit the stored value of a single variable */}
      {editingValueVar && (
        <EnvironmentVariableDialog
          open
          onOpenChange={(isOpen) => {
            if (!isOpen) setEditingValueVar(null);
          }}
          environmentSlug={editingValueVar.environmentSlug}
          entry={{
            name: editingValueVar.envVar.key,
            value: getValueForEnvironment(
              editingValueVar.envVar,
              editingValueVar.environmentSlug,
            ),
            isSecret: isSecretInEnvironment(
              editingValueVar.envVar,
              editingValueVar.environmentSlug,
            ),
          }}
          entryStored={hasEntryInEnvironment(
            editingValueVar.envVar,
            editingValueVar.environmentSlug,
          )}
          existingNames={[]}
          onSaved={() => {
            void invalidateAllListEnvironments(queryClient);
            setEditingValueVar(null);
          }}
        />
      )}

      {/* Confirm deleting a stored value */}
      <Dialog
        open={deletingValueVar !== null}
        onOpenChange={(isOpen) => {
          if (!isOpen) setDeletingValueVar(null);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete Variable</Dialog.Title>
            <Dialog.Description>
              Are you sure you want to delete{" "}
              <strong>{deletingValueVar?.name}</strong>? This action is
              permanent.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setDeletingValueVar(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() => {
                if (deletingValueVar) confirmDeleteValue(deletingValueVar);
              }}
            >
              Delete
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>

      {/* Add New Variable Sheet */}
      <AddVariableSheet
        open={isAddingNew}
        onOpenChange={setIsAddingNew}
        attachedEnvironment={attachedEnvironment || null}
        availableEnvVarsFromAttached={availableEnvVarsFromAttached}
        onAddVariables={handleAddVariables}
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
              <Label className="mb-2 block text-sm font-medium">
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
    </Stack>
  );
}

type OAuthSectionProps = {
  toolset: Toolset;
};

/**
 * Dispatches between the user-sessions surface and the legacy OAuth section
 * by the toolset's auth state (see toolsetAuthSurface). A wired issuer or a
 * clean slate gets the shared section; legacy OAuth keeps the old UI plus a
 * convert path.
 */
function OAuthSection({ toolset }: OAuthSectionProps) {
  const telemetry = useTelemetry();
  const oauthParadigm = getOAuthParadigm(toolset);
  const surface = toolsetAuthSurface({
    flagEnabled:
      telemetry.isFeatureEnabled(ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG) ??
      false,
    userSessionIssuerWired: isUserSessionIssuerWired(toolset),
    oauthParadigm,
  });

  if (surface === "manage" || surface === "attach") {
    return <ToolsetAuthenticationSection toolset={toolset} />;
  }
  return (
    <LegacyOAuthSection
      toolset={toolset}
      convertAction={
        surface === "legacy" ? toolsetConvertAction(oauthParadigm) : null
      }
    />
  );
}

function LegacyOAuthSection({
  toolset,
  convertAction,
}: OAuthSectionProps & {
  /** Migration entry point to render; null when the flag is off. */
  convertAction: ToolsetConvertAction | null;
}) {
  const [isOAuthModalOpen, setIsOAuthModalOpen] = useState(false);
  const [isOAuthDetailsModalOpen, setIsOAuthDetailsModalOpen] = useState(false);

  const loginSecured = !!toolset.userSessionIssuerSlug;
  const isOAuthConnected = !!toolset?.externalOauthServer;
  const availableOAuthAuthCode =
    toolset?.oauthEnablementMetadata?.oauth2SecurityCount > 0;
  const externalMcpOAuthStatus = useExternalMcpOAuthConfigStatus(toolset.slug);
  const externalMcpRequiresOAuth =
    externalMcpOAuthStatus === "required-unconfigured";
  const isOAuthEligible =
    toolset.mcpEnabled &&
    ((toolset.mcpIsPublic ? availableOAuthAuthCode : true) ||
      externalMcpRequiresOAuth);

  const oauthParadigm = getOAuthParadigm(toolset);

  const handleConfigureClick = () => {
    if (isOAuthConnected) return setIsOAuthDetailsModalOpen(true);
    setIsOAuthModalOpen(true);
  };

  const disabledTooltipText = !toolset.mcpEnabled
    ? "Enable the MCP server to configure OAuth"
    : "This MCP server does not require the OAuth authorization code flow";

  // Flag holders with a wired issuer never reach this component (the
  // dispatcher sends them to the manage surface), but non-holders can land
  // here wired, so the legacy display still handles it.
  const userSessionIssuerWired = !!toolset.userSessionIssuerSlug;
  // Once wired, the external OAuth config is inert — hide Configure so
  // operators aren't steered back into the legacy paradigm.
  const hideConfigureButton = userSessionIssuerWired;

  return (
    <PageSection
      heading="OAuth"
      description="OAuth let's you control access to MCP servers through an identity provider."
      headingExtra={undefined}
      action={
        <div className="flex items-center gap-2">
          {userSessionIssuerWired && (
            <Badge variant="success">
              <Badge.LeftIcon>
                <CheckCircle className="h-3.5 w-3.5" />
              </Badge.LeftIcon>
              <Badge.Text>Login Secured</Badge.Text>
            </Badge>
          )}
          {convertAction === "attach-sheet" && (
            <ConvertToUserSessionsButton toolset={toolset} />
          )}
          {!hideConfigureButton && (
            <Tooltip>
              <TooltipTrigger asChild>
                {!isOAuthEligible ? (
                  <span className="inline-block">
                    <Button disabled>
                      <Button.Text>Configure</Button.Text>
                    </Button>
                  </span>
                ) : (
                  <Button onClick={handleConfigureClick}>
                    <Button.Text>
                      {isOAuthConnected ? "Manage" : "Configure"}
                    </Button.Text>
                  </Button>
                )}
              </TooltipTrigger>
              {!isOAuthEligible && (
                <TooltipContent>{disabledTooltipText}</TooltipContent>
              )}
            </Tooltip>
          )}
        </div>
      }
    >
      <OAuthStatusDisplay
        isOAuthConnected={isOAuthConnected}
        isOAuthEligible={!!isOAuthEligible}
        externalMcpRequiresOAuth={externalMcpRequiresOAuth}
        loginSecured={loginSecured}
        showConfigureAction={!hideConfigureButton}
        oauthParadigm={oauthParadigm}
        mcpEnabled={!!toolset.mcpEnabled}
        onConfigureClick={handleConfigureClick}
      />

      {/* OAuth Modals */}
      <OAuthDetailsModal
        isOpen={isOAuthDetailsModalOpen}
        onClose={() => setIsOAuthDetailsModalOpen(false)}
        toolset={toolset}
      />
      <ConnectOAuthModal
        isOpen={isOAuthModalOpen}
        onClose={() => setIsOAuthModalOpen(false)}
        toolsetSlug={toolset.slug}
        toolset={toolset}
      />
    </PageSection>
  );
}

const PARADIGM_LABELS: Record<OAuthParadigm, string> = {
  external: "External OAuth",
};

function OAuthStatusDisplay({
  isOAuthConnected,
  isOAuthEligible,
  externalMcpRequiresOAuth,
  loginSecured,
  showConfigureAction,
  oauthParadigm,
  mcpEnabled,
  onConfigureClick,
}: {
  isOAuthConnected: boolean;
  isOAuthEligible: boolean;
  externalMcpRequiresOAuth: boolean;
  loginSecured: boolean;
  showConfigureAction: boolean;
  oauthParadigm: OAuthParadigm | null;
  mcpEnabled: boolean;
  onConfigureClick: () => void;
}) {
  if (loginSecured) {
    return (
      <div className="border-success-softest bg-success-softest rounded-lg border border-dashed p-8 text-center">
        <p className="text-success-foreground mb-1">
          <CheckCircle className="text-success-foreground mx-auto mb-1 h-5 w-5" />
          Login Secured
        </p>
        <p className="text-success-foreground text-sm">
          Users will authenticate with interactive auth before accessing this
          MCP server.
        </p>
      </div>
    );
  }

  if (isOAuthConnected && oauthParadigm) {
    return (
      <div className="border-success-softest bg-success-softest rounded-lg border border-dashed p-8 text-center">
        <p className="text-success-foreground mb-1">
          <CheckCircle className="text-success-foreground mx-auto mb-1 h-5 w-5" />
          {PARADIGM_LABELS[oauthParadigm]} is configured
        </p>
        <p className="text-success-foreground text-sm">
          Users will authenticate with your external OAuth server before
          accessing this MCP server.
        </p>
      </div>
    );
  }

  if (externalMcpRequiresOAuth) {
    return (
      <div className="border-warning-foreground/80 bg-warning dark:bg-warning/10 dark:border-warning-foreground/30 rounded-lg border border-dashed px-6 py-8 text-center">
        <AlertTriangle className="text-warning mx-auto mb-1 h-5 w-5" />
        <p className="text-warning-foreground mb-1 font-bold">
          OAuth setup required
        </p>
        <p className="text-warning-foreground/80 mb-3 text-sm">
          This MCP server requires OAuth configuration before it can be used.
        </p>
        {showConfigureAction && (
          <Button variant="secondary" onClick={onConfigureClick}>
            <Button.Text>Configure OAuth</Button.Text>
          </Button>
        )}
      </div>
    );
  }

  if (isOAuthEligible) {
    return (
      <div className="rounded-lg border border-dashed p-4 text-center">
        <p className="text-muted-foreground mb-1">
          <Shield className="text-muted-foreground mx-auto mb-1 h-5 w-5" />
          OAuth is available but not configured
        </p>
        <p className="text-muted-foreground mb-3 text-sm">
          Enable OAuth to require users to authenticate before accessing this
          MCP server.
        </p>
        {showConfigureAction && (
          <Button variant="secondary" onClick={onConfigureClick}>
            <Button.Text>Configure OAuth</Button.Text>
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-dashed px-6 py-8 text-center">
      <Shield className="text-muted-foreground mx-auto mb-3 h-6 w-6" />
      <p className="text-muted-foreground mb-2 font-medium">
        OAuth is not applicable
      </p>
      {!mcpEnabled ? (
        <p className="text-muted-foreground text-sm">
          Enable the MCP server to configure OAuth.
        </p>
      ) : (
        <p className="text-muted-foreground mx-auto max-w-lg text-sm">
          OAuth cannot be configured because there are no tools in this server
          that require OAuth authentication. OAuth is available for public MCP
          servers that have at least one tool requiring OAuth authentication, or
          private servers (using Speakeasy as an auth provider).
        </p>
      )}
    </div>
  );
}
