import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantMCPServerRef } from "@gram/client/models/components/assistantmcpserverref.js";
import { AssistantToolsetRef } from "@gram/client/models/components/assistanttoolsetref.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";

// The Select can't represent "no environment" with an empty value, so the
// project-default choice gets a sentinel that is stripped before saving.
const DEFAULT_ENV = "__default__";

type AttachedRef =
  | { kind: "toolset"; slug: string; environmentSlug?: string | undefined }
  | { kind: "mcpServer"; slug: string; environmentSlug?: string | undefined };

function attachedRefs(assistant: Assistant): AttachedRef[] {
  return [
    ...assistant.toolsets.map((t): AttachedRef => ({
      kind: "toolset",
      slug: t.toolsetSlug,
      environmentSlug: t.environmentSlug,
    })),
    ...(assistant.mcpServers ?? []).map((m): AttachedRef => ({
      kind: "mcpServer",
      slug: m.mcpServerSlug,
      environmentSlug: m.environmentSlug,
    })),
  ];
}

function toForms(refs: AttachedRef[]): {
  toolsets: AssistantToolsetRef[];
  mcpServers: AssistantMCPServerRef[];
} {
  return {
    toolsets: refs
      .filter((ref) => ref.kind === "toolset")
      .map((ref) => ({
        toolsetSlug: ref.slug,
        environmentSlug: ref.environmentSlug,
      })),
    mcpServers: refs
      .filter((ref) => ref.kind === "mcpServer")
      .map((ref) => ({
        mcpServerSlug: ref.slug,
        environmentSlug: ref.environmentSlug,
      })),
  };
}

/**
 * The MCP Servers section of the assistant detail panel, editable in the same
 * shape as the Skills section: an Add picker, and per-row environment select +
 * Remove. Attachments save through the assistant update endpoint, which
 * replaces the toolset and MCP server arrays wholesale.
 */
export function AssistantMCPServersSection({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);

  const attached = attachedRefs(assistant);

  const gramProject = useProjectSlugForRequests();
  const { data: environmentsResult } = useListEnvironments(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const environments = environmentsResult?.environments ?? [];

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
    },
    onError: () => {
      toast.error("Failed to update MCP servers");
    },
  });

  const saveRefs = (refs: AttachedRef[], successMessage: string) => {
    const { toolsets, mcpServers } = toForms(refs);
    update.mutate(
      {
        request: {
          updateAssistantForm: { id: assistant.id, toolsets, mcpServers },
        },
      },
      {
        onSuccess: () => {
          toast.success(successMessage);
        },
      },
    );
  };

  const removeRef = (ref: AttachedRef) => {
    saveRefs(
      attached.filter((r) => !(r.kind === ref.kind && r.slug === ref.slug)),
      "MCP server detached",
    );
  };

  const setRefEnvironment = (ref: AttachedRef, environmentSlug?: string) => {
    saveRefs(
      attached.map((r) =>
        r.kind === ref.kind && r.slug === ref.slug
          ? { ...r, environmentSlug }
          : r,
      ),
      "Environment updated",
    );
  };

  const addRefs = (refs: AttachedRef[]) => {
    saveRefs(
      [...attached, ...refs],
      refs.length > 1
        ? `${refs.length} MCP servers attached`
        : "MCP server attached",
    );
  };

  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="text-eyebrow">MCP Servers ({attached.length})</div>
        <RequireScope
          scope="project:write"
          resourceId={project.id}
          level="component"
          reason="You need project write access to attach MCP servers."
        >
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setAddOpen(true)}
            disabled={update.isPending}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-3 w-3" />
            </Button.LeftIcon>
            <Button.Text>Add</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {attached.length === 0 ? (
        <Text small muted>
          No MCP servers attached.
        </Text>
      ) : (
        <Stack gap={2}>
          {attached.map((ref) => (
            <AttachedServerRow
              key={`${ref.kind}:${ref.slug}`}
              serverRef={ref}
              environments={environments.map((env) => env.slug)}
              disabled={update.isPending}
              onEnvironmentChange={(environmentSlug) =>
                setRefEnvironment(ref, environmentSlug)
              }
              onRemove={() => removeRef(ref)}
            />
          ))}
        </Stack>
      )}

      <AddServersDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        attached={attached}
        saving={update.isPending}
        onAdd={addRefs}
      />
    </div>
  );
}

function AttachedServerRow({
  serverRef,
  environments,
  disabled,
  onEnvironmentChange,
  onRemove,
}: {
  serverRef: AttachedRef;
  environments: string[];
  disabled: boolean;
  onEnvironmentChange: (environmentSlug?: string) => void;
  onRemove: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();

  const link =
    serverRef.kind === "toolset" ? (
      <routes.mcp.details.Link
        params={[serverRef.slug]}
        className="min-w-0 hover:no-underline"
      >
        <code className="truncate text-xs">{serverRef.slug}</code>
      </routes.mcp.details.Link>
    ) : (
      <routes.mcp.x.Link
        params={[serverRef.slug]}
        className="min-w-0 hover:no-underline"
      >
        <code className="truncate text-xs">{serverRef.slug}</code>
      </routes.mcp.x.Link>
    );

  return (
    <div className="border-border border px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        {link}
        <Icon
          name="chevron-right"
          className="text-muted-foreground h-4 w-4 shrink-0"
        />
      </div>
      <RequireScope
        scope="project:write"
        resourceId={project.id}
        level="component"
        className="mt-2 w-full"
      >
        <div className="flex w-full items-center gap-2">
          <Select
            value={serverRef.environmentSlug ?? DEFAULT_ENV}
            onValueChange={(value) =>
              onEnvironmentChange(value === DEFAULT_ENV ? undefined : value)
            }
            disabled={disabled}
          >
            <SelectTrigger
              size="sm"
              className="min-w-0 flex-1"
              aria-label={`Environment for ${serverRef.slug}`}
            >
              <SelectValue>
                {serverRef.environmentSlug
                  ? `env: ${serverRef.environmentSlug}`
                  : "Default environment"}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={DEFAULT_ENV}>Default environment</SelectItem>
              {environments.map((slug) => (
                <SelectItem key={slug} value={slug}>
                  {slug}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={onRemove}
            aria-label={`Remove ${serverRef.slug}`}
          >
            Remove
          </Button>
        </div>
      </RequireScope>
    </div>
  );
}

function AddServersDialog({
  open,
  onOpenChange,
  attached,
  saving,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  attached: AttachedRef[];
  saving: boolean;
  onAdd: (refs: AttachedRef[]) => void;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const toolsets = useToolsets();
  const {
    data: mcpServersResult,
    isLoading,
    isError: isServersError,
  } = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
    enabled: open,
  });
  const {
    data: endpointsResult,
    isLoading: isLoadingEndpoints,
    isError: isEndpointsError,
  } = useMcpEndpoints({ gramProject }, undefined, {
    throwOnError: false,
    enabled: open,
  });
  const [selected, setSelected] = useState<Map<string, AttachedRef>>(new Map());

  const attachedKeys = useMemo(
    () => new Set(attached.map((ref) => `${ref.kind}:${ref.slug}`)),
    [attached],
  );

  const options = useMemo(() => {
    // Mirror the attach-time validation in the assistants service: tunneled
    // backends, disabled servers, and servers without a Gram-hosted endpoint
    // are rejected on write, so don't offer them.
    const serverIdsWithGramEndpoint = new Set(
      (endpointsResult?.mcpEndpoints ?? [])
        .filter((endpoint) => !endpoint.customDomainId)
        .map((endpoint) => endpoint.mcpServerId),
    );
    const toolsetOptions = toolsets.map((t): AttachedRef => ({
      kind: "toolset",
      slug: t.slug,
    }));
    const serverOptions = (mcpServersResult?.mcpServers ?? [])
      .filter(
        (server) =>
          !!server.slug &&
          !server.tunneledMcpServerId &&
          server.visibility !== "disabled" &&
          // Rejected by resolveMcpServerRefsForWrite for the same reason
          // tunneled servers are: the assistant runtime sends only
          // Gram-Environment and no Authorization header, so an upstream
          // server would 401 on every turn. Offering it here would surface
          // that as a validation error after the user picked it.
          server.visibility !== "upstream" &&
          serverIdsWithGramEndpoint.has(server.id),
      )
      .map((server): AttachedRef => ({
        kind: "mcpServer",
        slug: server.slug ?? "",
      }));
    return [...toolsetOptions, ...serverOptions].filter(
      (ref) => !attachedKeys.has(`${ref.kind}:${ref.slug}`),
    );
  }, [toolsets, mcpServersResult, endpointsResult, attachedKeys]);

  const toggle = (ref: AttachedRef) => {
    const key = `${ref.kind}:${ref.slug}`;
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.set(key, ref);
      }
      return next;
    });
  };

  const close = (nextOpen: boolean) => {
    if (!nextOpen) setSelected(new Map());
    onOpenChange(nextOpen);
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <Dialog.Content className="flex max-h-[70vh] w-[min(90vw,480px)] flex-col gap-4">
        <Dialog.Title>Attach MCP Servers</Dialog.Title>
        <Dialog.Description>
          Give this assistant access to the project's MCP servers and hosted
          toolsets.
        </Dialog.Description>

        <div className="min-h-0 flex-1 overflow-y-auto">
          <DialogOptionsBody
            isLoading={isLoading || isLoadingEndpoints || toolsets.isLoading}
            isError={isServersError || isEndpointsError || toolsets.isError}
            options={options}
            selected={selected}
            onToggle={toggle}
          />
        </div>

        <Dialog.Footer>
          <Button variant="secondary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button
            disabled={selected.size === 0 || saving}
            onClick={() => {
              onAdd([...selected.values()]);
              close(false);
            }}
          >
            Attach
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// Endpoint eligibility is unknowable while the endpoints query is loading or
// failed, so those states must not masquerade as "everything attached".
function DialogOptionsBody({
  isLoading,
  isError,
  options,
  selected,
  onToggle,
}: {
  isLoading: boolean;
  isError: boolean;
  options: AttachedRef[];
  selected: Map<string, AttachedRef>;
  onToggle: (ref: AttachedRef) => void;
}): JSX.Element {
  if (isLoading) {
    return (
      <Text small muted>
        Loading servers…
      </Text>
    );
  }
  if (isError) {
    return (
      <Text small muted>
        Couldn't load the project's MCP servers and toolsets. Close the dialog
        and try again.
      </Text>
    );
  }
  if (options.length === 0) {
    return (
      <Text small muted>
        Every MCP server in this project is already attached.
      </Text>
    );
  }
  return (
    <Stack gap={1}>
      {options.map((ref) => {
        const key = `${ref.kind}:${ref.slug}`;
        return (
          <label
            key={key}
            className="hover:bg-muted/50 flex cursor-pointer items-center gap-2 border border-transparent px-2 py-1.5"
          >
            <Checkbox
              checked={selected.has(key)}
              onCheckedChange={() => onToggle(ref)}
            />
            <code className="truncate text-xs">{ref.slug}</code>
          </label>
        );
      })}
    </Stack>
  );
}
