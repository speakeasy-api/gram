import { useOrganization } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useFleetParamUpdate } from "./useFleetParamUpdate";
import { restoreFleetFocus } from "./fleet-focus";
import { useEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router";
import { ArrowLeft, ArrowUpRight } from "lucide-react";
import { Button } from "@/components/ui/Button";
import {
  Tabs,
  TabsContent,
  PageTabsList,
  PageTabsTrigger,
} from "@/components/ui/Tabs";
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/Sheet";
import { IdentityLink } from "@/components/identity-link";
import { useIsMobile } from "@/hooks/use-mobile";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { useRoutes } from "@/routes";
import { AgentPolicySection } from "@/pages/agents/AgentPolicySection";
import { ManagedAgentSessions } from "@/pages/agents/ManagedAgentSessions";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { AgentMCPControls } from "./AgentMCPControls";
import { FleetStatus } from "./FleetCollection";
import type { FleetRow } from "./fleet-model";

export function FleetInspector({
  row,
  rows,
  onClose,
  onSelect,
  blocked,
}: {
  row: FleetRow;
  rows: FleetRow[];
  onClose: () => void;
  onSelect: (id: string) => void;
  blocked: boolean;
}): JSX.Element {
  const mobile = useIsMobile();
  const content = (
    <InspectorContents
      key={row.id}
      row={row}
      rows={rows}
      onClose={onClose}
      onSelect={onSelect}
      blocked={blocked}
    />
  );
  if (mobile)
    return (
      <Sheet
        open
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      >
        <SheetContent
          data-fleet-inspector
          className="w-full overflow-y-auto p-0 sm:max-w-none"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            restoreFleetFocus(row.id);
          }}
        >
          <SheetTitle className="sr-only">{row.title}</SheetTitle>
          <SheetDescription className="sr-only">
            Selected Fleet item
          </SheetDescription>
          {content}
        </SheetContent>
      </Sheet>
    );
  return (
    <aside className="fleet-inspector" aria-label="Selected Fleet item">
      {content}
    </aside>
  );
}
function InspectorContents({
  row,
  rows,
  onClose,
  onSelect,
  blocked,
}: {
  row: FleetRow;
  rows: FleetRow[];
  onClose: () => void;
  onSelect: (id: string) => void;
  blocked: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const [params] = useSearchParams();
  const update = useFleetParamUpdate();
  const access = useKillswitchAccess();
  const isDemo = useOrganization().slug === DEMO_ORG_SLUG;
  const heading = useRef<HTMLHeadingElement>(null);
  const chatActions = useChatDetailSheet();
  const requestedTab = params.get("detail");
  const tab =
    requestedTab === "identity" || requestedTab === "controls"
      ? requestedTab
      : "activity";
  useEffect(() => {
    heading.current?.focus({ preventScroll: true });
  }, [row.id]);
  const assistantId = row.assistant?.id ?? row.session?.assistantId;
  const siblings = rows.filter(
    (candidate) =>
      candidate.session &&
      assistantId &&
      candidate.session.assistantId === assistantId,
  );
  return (
    <>
      <div className="fleet-inspector-top">
        <Button variant="tertiary" size="sm" onClick={onClose}>
          <Button.LeftIcon>
            <ArrowLeft size={15} />
          </Button.LeftIcon>
          <Button.Text>Back to Fleet</Button.Text>
        </Button>
      </div>
      <div className="fleet-inspector-body">
        <FleetStatus row={row} blocked={blocked} />
        <h2 ref={heading} tabIndex={-1} className="text-display-xs font-normal">
          {row.title}
        </h2>
        <p className="text-muted-foreground text-sm">{row.subtitle}</p>
        <dl className="fleet-facts">
          <Fact label={row.personRole}>
            <IdentityLink
              identifier={row.personId ? { userId: row.personId } : null}
            >
              {row.personName}
            </IdentityLink>
          </Fact>
          <Fact label="Department">{row.department}</Fact>
          <Fact label="Acting identity">
            <span className="break-all">{row.actingIdentity}</span>
          </Fact>
        </dl>
        <Tabs
          value={tab}
          onValueChange={(value) => update({ detail: value }, true)}
        >
          <PageTabsList aria-label="Fleet item details">
            <PageTabsTrigger value="activity">Activity</PageTabsTrigger>
            <PageTabsTrigger value="identity">Identity</PageTabsTrigger>
            <PageTabsTrigger value="controls">Controls</PageTabsTrigger>
          </PageTabsList>
          <TabsContent value="activity" className="space-y-5">
            {row.session && (
              <>
                <dl className="fleet-facts">
                  <Fact label="Last captured event">
                    {row.lastActivity?.toLocaleString()}
                  </Fact>
                  <Fact label="Captured messages">
                    {row.session.numMessages.toLocaleString()}
                  </Fact>
                  <Fact label="Risk findings">
                    {row.session.riskFindingsCount ?? "Not reported"}
                  </Fact>
                </dl>
                {row.session.summary && (
                  <p className="text-sm leading-relaxed">
                    {row.session.summary}
                  </p>
                )}
                <Button
                  variant="secondary"
                  onClick={() => chatActions.openChat(row.session!.id)}
                >
                  <Button.Text>
                    Open transcript and Security findings
                  </Button.Text>
                  <Button.RightIcon>
                    <ArrowUpRight size={14} />
                  </Button.RightIcon>
                </Button>
              </>
            )}
            {row.agent && (
              <>
                <dl className="fleet-facts">
                  <Fact label="Last credential authentication (organization-wide)">
                    {row.agent.lastCredentialUsedAt?.toLocaleString()}
                  </Fact>
                </dl>
                <p className="text-muted-foreground text-sm">
                  Latest recorded credential authentication at Gram. Credential
                  sessions for this identity:
                </p>
                {!isDemo ? (
                  <ManagedAgentSessions agent={row.agent} />
                ) : (
                  <p className="text-muted-foreground text-sm">
                    Credential details are unavailable in the read-only demo.
                  </p>
                )}
              </>
            )}
            {assistantId && (
              <section className="space-y-2">
                <h3 className="font-medium">
                  Captured sessions · {siblings.length} loaded
                </h3>
                {siblings.map((sibling) => (
                  <button
                    key={sibling.id}
                    className="fleet-sibling"
                    onClick={() => onSelect(sibling.id)}
                  >
                    <span>{sibling.title}</span>
                    <small>{sibling.lastActivity?.toLocaleString()}</small>
                  </button>
                ))}
                {!siblings.length && (
                  <p className="text-muted-foreground text-sm">
                    No sessions for this assistant are in the loaded page. Use
                    the assistant’s session history for the full list.
                  </p>
                )}
              </section>
            )}
            {row.assistant && (
              <Link
                className="fleet-text-link"
                to={routes.assistants.detail.href(row.assistant.id)}
              >
                Open assistant history
                <ArrowUpRight size={14} />
              </Link>
            )}
          </TabsContent>
          <TabsContent value="identity" className="space-y-5">
            <dl className="fleet-facts">
              <Fact label="Source">{row.subtitle}</Fact>
              {row.session && (
                <Fact label="Session ID">
                  <code>{row.session.id}</code>
                </Fact>
              )}
              <Fact label="Registered agent binding">
                {row.agent ? row.agent.id : "Not established"}
              </Fact>
              <Fact label="Initiation">Not reported</Fact>
              <Fact label="Execution state">Not reported</Fact>
            </dl>
            {row.session?.userId && (
              <p className="text-muted-foreground text-sm">
                Capture is attributed to user <code>{row.session.userId}</code>.
                That attribution does not establish the credential used for MCP
                calls.
              </p>
            )}
            {row.agent && (
              <>
                <dl className="fleet-facts">
                  <Fact label="Registration">{row.agent.lifecycle}</Fact>
                  <Fact label="Owner reassignment">
                    {row.agent.ownerReassignmentRequiredAt
                      ? "Required"
                      : "Not required"}
                  </Fact>
                  <Fact label="Created">
                    {row.agent.createdAt.toLocaleString()}
                  </Fact>
                </dl>
                {!isDemo && row.agent.permissions.write ? (
                  <AgentPolicySection agent={row.agent} />
                ) : (
                  <p className="text-muted-foreground text-sm">
                    {isDemo
                      ? "Policy details are unavailable in the read-only demo."
                      : "Policy details require agent write access."}
                  </p>
                )}
                {!isDemo && (
                  <Link
                    className="fleet-text-link"
                    to={`${routes.agents.href()}?id=${encodeURIComponent(row.agent.id)}`}
                  >
                    Manage agent identity and credentials
                    <ArrowUpRight size={14} />
                  </Link>
                )}
              </>
            )}
            {row.assistant && (
              <dl className="fleet-facts">
                <Fact label="Assistant">{row.assistant.name}</Fact>
                <Fact label="Model">{row.assistant.model}</Fact>
                <Fact label="Configuration">{row.assistant.status}</Fact>
                <Fact label="MCP servers">
                  {row.assistant.mcpServers
                    .map((server) => server.mcpServerSlug)
                    .join(", ") || "None attached"}
                </Fact>
                <Fact label="Instructions">{row.assistant.instructions}</Fact>
              </dl>
            )}
          </TabsContent>
          <TabsContent value="controls" className="space-y-4">
            {row.agent && access.canAccess && (
              <AgentMCPControls agent={row.agent} />
            )}
            {row.agent && !access.canAccess && (
              <p className="text-muted-foreground text-sm">
                {access.reason === "demo"
                  ? "MCP kill switches are unavailable in the read-only demo."
                  : "MCP kill switches require an ordinary organization-administrator session and enabled kill-switch access."}
              </p>
            )}
            {!row.agent && (
              <p className="text-muted-foreground text-sm">
                No registered-agent credential binding is available for this
                item. MCP Kill cannot safely target it. User attribution is
                separate from the acting credential.
              </p>
            )}
            {row.agent && !isDemo && (
              <Link
                className="fleet-text-link"
                to={`${routes.agents.href()}?id=${encodeURIComponent(row.agent.id)}`}
              >
                Suspend / resume registration or revoke credentials
                <ArrowUpRight size={14} />
              </Link>
            )}
            <p className="text-muted-foreground text-xs">
              MCP blocked describes covered tools/call restrictions. It does not
              report process state or stop a runtime.
            </p>
          </TabsContent>
        </Tabs>
      </div>
      {chatActions.sheet}
    </>
  );
}
function Fact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
