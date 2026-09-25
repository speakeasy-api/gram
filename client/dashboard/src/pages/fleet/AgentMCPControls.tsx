import { useSearchParams } from "react-router";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { KillswitchEditorSheet } from "@/components/killswitch/KillswitchEditorSheet";
import {
  draftTarget,
  draftToSchedule,
  draftToScope,
  type EditorDraft,
} from "@/components/killswitch/killswitch-view-model";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import { useKillswitchCapabilities } from "@gram/client/react-query/killswitchCapabilities.js";
import { invalidateAllKillswitches } from "@gram/client/react-query/killswitches.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentRestrictions } from "./AgentRestrictions";

export function AgentMCPControls({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const [, setParams] = useSearchParams();
  const [open, setOpen] = useState(false);
  const session = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const security = { sessionHeaderGramSession: session.session };
  const servers = useKillswitchMCPServers(
    security,
    { gramSession: session.session },
    { throwOnError: false },
  );
  const capabilities = useKillswitchCapabilities(
    security,
    { gramSession: session.session },
    { enabled: open, throwOnError: false },
  );
  const request = (draft: EditorDraft) => ({
    ...draftTarget(draft),
    capabilityKey: "mcp_tool_calls" as const,
    scope: draftToScope(draft),
    schedule: draftToSchedule(draft),
  });
  return (
    <div className="space-y-5">
      <p className="text-muted-foreground text-sm">
        MCP restrictions apply to this registered agent across all its
        credential sessions in the organization. Runtime state is independent.
      </p>
      <Button
        variant="destructive-primary"
        disabled={
          agent.lifecycle === "revoked" ||
          servers.isLoading ||
          Boolean(servers.error)
        }
        onClick={() => setOpen(true)}
      >
        Kill MCP access
      </Button>
      {servers.error && (
        <p role="alert" className="text-sm">
          Couldn’t load eligible servers.{" "}
          <Button variant="tertiary" onClick={() => void servers.refetch()}>
            Try again
          </Button>
        </p>
      )}
      <KillswitchEditorSheet
        open={open}
        onOpenChange={setOpen}
        mode="create"
        agentTarget={agent}
        members={[]}
        servers={servers.data?.servers ?? []}
        capabilities={capabilities.data?.capabilities ?? []}
        comingSoon={[]}
        capabilitiesLoading={capabilities.isLoading}
        capabilitiesError={capabilities.error}
        onRetryCapabilities={() => void capabilities.refetch()}
        onView={(id) => {
          setOpen(false);
          setParams(
            (previous) => {
              const next = new URLSearchParams(previous);
              next.set("restriction", id);
              return next;
            },
            { replace: true },
          );
        }}
        onPreview={(draft) =>
          sdk.killswitches.previewOverlaps(security, {
            killswitchPreviewOverlapsRequest: request(draft),
          })
        }
        onSubmit={async (draft, operationId) => {
          const result = await sdk.killswitches.create(security, {
            killswitchCreateRequest: {
              ...request(draft),
              operationId,
              externalNote: draft.externalNote,
              internalNote: draft.internalNote,
            },
          });
          await Promise.all([
            invalidateAllKillswitches(queryClient),
            queryClient.invalidateQueries({ queryKey: ["fleet-agent-badges"] }),
          ]);
          return result;
        }}
      />
      {/* A single-agent inventory is safe only with the exact agentId guard. */}
      <AgentRestrictions
        agents={[agent]}
        inventoryAvailable
        agentId={agent.id}
      />
    </div>
  );
}
