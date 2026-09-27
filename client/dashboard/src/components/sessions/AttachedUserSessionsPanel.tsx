import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { Stack } from "@/components/ui/Stack";

export interface AttachableSession {
  id: string;
  label: string;
  status: string;
  agentIds: string[];
}
interface BindingAgent {
  id: string;
  name: string;
  eligible: boolean;
}

export interface AttachedUserSessionsPanelProps {
  sessions: AttachableSession[];
  agents: BindingAgent[];
  eligibleAgentIdsBySession?: Record<string, string[]>;
  isLoading: boolean;
  isError: boolean;
  connectUrl?: string;
  emptyMessage?: string;
  onRefresh: () => void;
  onSave: (sessionId: string, agentIds: string[]) => Promise<void>;
}

/** This panel only changes bindings. It never changes an agent's credential policy. */
export function AttachedUserSessionsPanel(
  props: AttachedUserSessionsPanelProps,
): JSX.Element {
  const [sessionId, setSessionId] = useState("");
  const session = props.sessions.find((item) => item.id === sessionId);
  return (
    <Stack gap={3} className="rounded-lg border p-4">
      <Stack gap={1}>
        <Text variant="subheading">Attached user sessions</Text>
        <Text small muted>
          Connect your upstream account, then choose which agents can use it on
          this MCP server. Agent credential policies are managed separately.
        </Text>
      </Stack>
      <Stack direction="horizontal" gap={2}>
        <Button
          disabled={!props.connectUrl}
          onClick={() => {
            if (props.connectUrl)
              window.open(props.connectUrl, "_blank", "noopener,noreferrer");
          }}
        >
          Connect account
        </Button>
        <Button
          variant="secondary"
          disabled={props.isLoading}
          onClick={props.onRefresh}
        >
          Refresh accounts
        </Button>
      </Stack>
      <Text small muted>
        After connecting in the new tab, refresh accounts here. Only your own
        upstream sessions are available.
      </Text>
      {props.isLoading ? (
        <Text role="status">Loading accounts and agents…</Text>
      ) : props.isError ? (
        <Text role="alert">
          Could not load your accounts or eligible agents. Refresh accounts to
          try again.
        </Text>
      ) : props.sessions.length === 0 ? (
        <Text small muted>
          {props.emptyMessage ??
            "No upstream accounts connected for this server yet."}
        </Text>
      ) : (
        <>
          <label className="flex flex-col gap-2 text-sm">
            Your upstream session
            <select
              className="rounded-md border bg-background p-2"
              value={session?.id ?? ""}
              onChange={(event) => setSessionId(event.target.value)}
            >
              <option value="">Choose an account</option>
              {props.sessions.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.label} ·{" "}
                  {item.status === "active"
                    ? "available to attach"
                    : item.status}
                </option>
              ))}
            </select>
          </label>
          {session && (
            <SessionAgents
              key={`${session.id}:${session.status}:${session.agentIds.join(",")}`}
              session={session}
              agents={props.agents.map((agent) => ({
                ...agent,
                eligible:
                  agent.eligible &&
                  (!props.eligibleAgentIdsBySession ||
                    !!props.eligibleAgentIdsBySession[session.id]?.includes(
                      agent.id,
                    )),
              }))}
              onSave={props.onSave}
            />
          )}
        </>
      )}
      <Text small muted>
        To replace an account for the same upstream client, detach the old
        account first. Reconnecting an account also requires detaching and
        reattaching its agents to authorize the new grant. Detaching removes an
        agent's access through this binding; it does not revoke the shared
        upstream session. Revoking that session stops access for every agent
        using it. Other credentials or bindings may still grant access.
      </Text>
    </Stack>
  );
}

function SessionAgents({
  session,
  agents,
  onSave,
}: {
  session: AttachableSession;
  agents: BindingAgent[];
  onSave: AttachedUserSessionsPanelProps["onSave"];
}) {
  const [selected, setSelected] = useState(session.agentIds);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const [saved, setSaved] = useState(false);
  const selectable = agents.filter(
    (agent) => agent.eligible || session.agentIds.includes(agent.id),
  );
  const dirty =
    selected.length !== session.agentIds.length ||
    selected.some((id) => !session.agentIds.includes(id));
  async function save() {
    setPending(true);
    setError(false);
    setSaved(false);
    try {
      await onSave(session.id, selected);
      setSaved(true);
    } catch {
      setError(true);
    } finally {
      setPending(false);
    }
  }
  return (
    <Stack gap={3}>
      {session.status !== "active" && (
        <Text role="status" small muted>
          This session is not available for new attachments. Refresh accounts or
          reconnect before attaching more agents. Existing bindings can still be
          detached.
        </Text>
      )}
      <fieldset disabled={pending} className="space-y-2">
        <legend className="mb-2 text-sm font-medium">Attached agents</legend>
        {selectable.length === 0 && (
          <Text small muted>
            No eligible agents. You must own an active agent to attach a
            session.
          </Text>
        )}
        {selectable.map((agent) => (
          <label key={agent.id} className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={selected.includes(agent.id)}
              disabled={
                !session.agentIds.includes(agent.id) &&
                (!agent.eligible || session.status !== "active")
              }
              onCheckedChange={(checked) => {
                setSaved(false);
                setSelected((ids) =>
                  checked === true
                    ? [...ids, agent.id]
                    : ids.filter((id) => id !== agent.id),
                );
              }}
            />
            {agent.name}
            {!agent.eligible && (
              <span className="text-muted-foreground">
                Unavailable for new attachments
              </span>
            )}
          </label>
        ))}
      </fieldset>
      <Button disabled={pending || error || !dirty} onClick={() => void save()}>
        {pending ? "Saving…" : "Save attachments"}
      </Button>
      {error && (
        <Text role="alert">
          Could not save all attachments. Refresh accounts to check current
          access before trying again.
        </Text>
      )}
      {saved && <Text role="status">Attachments saved.</Text>}
    </Stack>
  );
}
