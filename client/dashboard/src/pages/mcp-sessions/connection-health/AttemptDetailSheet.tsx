import { IdentityAvatar } from "@/components/identity-avatar";
import { InlineEmptyState } from "@/components/inline-empty-state";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Skeleton } from "@/components/ui/Skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";
import { ArrowRight, ChevronDown, Globe, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { connectionHealthNow, useAttemptHere } from "./api";
import { AskAiButton, type Investigation } from "./AskAiButton";
import { OutcomeLabel } from "./AttemptsTable";
import { logsSearch, RETENTION_MS } from "./filters";
import {
  appLabel,
  formatClock,
  formatDayMonth,
  formatFull,
  outcomeLabel,
  personLabel,
  plural,
  serverLabel,
  SOURCE_LABELS,
  STEP_LABELS,
  stepResultLabel,
} from "./format";
import type { AttemptDetail } from "./types";

/** One attempt, opened from a Connection Logs row or its menu. */
export function AttemptDetailSheet({
  reference,
  providerId,
  providerName,
  lockedServerId,
  onClose,
}: {
  reference: string | null;
  providerId: string;
  providerName?: string;
  /** On a server's tab, only that server's attempts open. */
  lockedServerId?: string;
  onClose: () => void;
}): JSX.Element {
  const { query, attempt, elsewhere, failed, isPending, retry } =
    useAttemptHere(reference, providerId, lockedServerId);
  return (
    <Sheet
      open={!!reference}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="bg-card w-full gap-0 overflow-y-auto p-0 sm:max-w-[600px]">
        {attempt ? (
          <AttemptDetailBody
            attempt={attempt}
            providerId={providerId}
            providerName={providerName}
            onClose={onClose}
          />
        ) : (
          <>
            <SheetHeader className="border-b px-6 py-5">
              <SheetTitle className="sr-only">Connection attempt</SheetTitle>
              <SheetDescription className="sr-only">
                One connection attempt
              </SheetDescription>
            </SheetHeader>
            <div className="p-6">
              {isPending ? (
                <div className="space-y-3">
                  <Skeleton className="h-6 w-2/3" />
                  <Skeleton className="h-24 w-full" />
                  <Skeleton className="h-32 w-full" />
                </div>
              ) : (
                <InlineEmptyState
                  icon="search"
                  heading={
                    failed
                      ? "Couldn't load this attempt"
                      : elsewhere
                        ? lockedServerId
                          ? "This attempt is on another server"
                          : "This attempt is through another provider"
                        : query.data?.loggingEnabled === false
                          ? "Logging is off"
                          : "No attempt with this reference"
                  }
                  description={
                    failed
                      ? "Connections are not affected."
                      : elsewhere
                        ? "Search for its reference in that view to open it."
                        : query.data?.loggingEnabled === false
                          ? "Attempts are not kept while logging is off for this organization."
                          : query.data?.viewerScope === "own"
                            ? "Connection Logs keep the last 30 days, and you see only your own attempts."
                            : "Connection Logs keep the last 30 days of this project."
                  }
                  action={
                    failed ? (
                      <Button variant="secondary" onClick={retry}>
                        Try again
                      </Button>
                    ) : undefined
                  }
                />
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function questionFor(attempt: AttemptDetail, who: string, where: string) {
  const to = where ? ` to ${where}` : "";
  switch (attempt.outcome) {
    case "connected":
      return `What happened in ${who}'s connection${to}, and does anything look wrong?`;
    case "in_progress":
      return `${who}'s connection${to} is still in progress. What has happened so far?`;
    case "failed":
    case "blocked":
    case "declined":
    case "didnt_finish":
    default:
      return `Why did ${who}'s connection${to} end as "${outcomeLabel(attempt.outcome)}", and is there anything I should change?`;
  }
}

/** The attempt's local day start, or null when that is older than the log. */
function startOfDay(iso: string): string | null {
  const date = new Date(iso);
  date.setHours(0, 0, 0, 0);
  return date.getTime() > Date.now() - RETENTION_MS ? date.toISOString() : null;
}

function investigationFor(attempt: AttemptDetail): Investigation {
  const who = personLabel(attempt);
  const where = serverLabel(attempt);
  return {
    question: questionFor(attempt, who, where),
    context: [
      `Connection attempt ${attempt.id}`,
      `${outcomeLabel(attempt.outcome)} at step ${STEP_LABELS[attempt.step]}, ${formatFull(attempt.time)}`,
      `Person: ${who}; app: ${appLabel(attempt.app)}; server: ${where}`,
      attempt.service ? `Service: ${attempt.service}` : null,
      attempt.message ? `Message: ${attempt.message}` : null,
      attempt.technicalMessage
        ? `Technical detail: ${attempt.technicalMessage}`
        : null,
      attempt.reason ? `Reason code: ${attempt.reason}` : null,
      attempt.source ? `Source: ${SOURCE_LABELS[attempt.source]}` : null,
      attempt.count > 1
        ? `Stands for ${attempt.count} attempts: this one and ${plural(attempt.count - 1, "repeat")}`
        : null,
    ]
      .filter(Boolean)
      .join("\n"),
  };
}

function AttemptDetailBody({
  attempt,
  providerId,
  providerName,
  onClose,
}: {
  attempt: AttemptDetail;
  providerId: string;
  providerName?: string;
  onClose: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const who = personLabel(attempt);
  const where = serverLabel(attempt);
  // The link filters by server, not gateway member.
  const gateway = attempt.server ? serverLabel({ server: attempt.server }) : "";
  const now = connectionHealthNow();
  const inLastDay =
    now.getTime() - Date.parse(attempt.startedAt) <
    24 * 60 * 60 * 1000 - 5 * 60 * 1000;
  const dayStart = inLastDay ? null : startOfDay(attempt.startedAt);
  return (
    <div className="flex min-h-full flex-col">
      <SheetHeader className="gap-2 border-b px-6 py-5 pr-12">
        <div className="text-eyebrow flex items-center gap-2">
          <OutcomeLabel outcome={attempt.outcome} className="text-foreground" />
          <span>{formatFull(attempt.time)}</span>
        </div>
        <SheetTitle className="font-mono text-lg font-normal">
          {where || "No server"} / {STEP_LABELS[attempt.step]}
        </SheetTitle>
        <SheetDescription asChild>
          <div className="mt-1 flex items-center gap-3">
            {attempt.person ? (
              <IdentityAvatar label={who} className="size-8" />
            ) : (
              <span className="text-muted-foreground flex size-8 shrink-0 items-center justify-center">
                <Globe className="size-4" aria-hidden />
              </span>
            )}
            <div className="min-w-0">
              <div className="text-foreground truncate text-sm">{who}</div>
              <div className="text-muted-foreground truncate text-xs">
                {[
                  attempt.person?.email ??
                    (attempt.network
                      ? `Network ${attempt.network.label}`
                      : null),
                  appLabel(attempt.app),
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </div>
            </div>
          </div>
        </SheetDescription>
      </SheetHeader>

      <Tabs defaultValue="details" className="flex-1 gap-0 px-6 py-5">
        <TabsList className="h-9 w-full p-0.5">
          <TabsTrigger value="details" className={SHEET_TAB}>
            Details
          </TabsTrigger>
          <TabsTrigger value="raw" className={SHEET_TAB}>
            Raw data
          </TabsTrigger>
        </TabsList>
        <TabsContent value="details" className="mt-5 space-y-6">
          {attempt.message || attempt.technicalMessage ? (
            <div className="border-foreground border-l-2 pl-3">
              <div className="text-eyebrow mb-1">Message</div>
              {attempt.message ? (
                <p className="text-sm">{attempt.message}</p>
              ) : null}
              {attempt.technicalMessage ? (
                <p className="text-muted-foreground mt-1 text-sm">
                  {attempt.technicalMessage}
                </p>
              ) : null}
            </div>
          ) : null}

          <Section title="Steps in this attempt">
            <div className="divide-y border">
              {attempt.steps.map((step) => (
                <div
                  key={`${step.at}-${step.storedStep}`}
                  className="grid grid-cols-[80px_1fr_1fr] items-center gap-3 px-4 py-2.5 text-sm"
                >
                  <span className="text-muted-foreground font-mono text-xs">
                    {formatClock(step.at)}
                  </span>
                  <span>{STEP_LABELS[step.step]}</span>
                  <span className="text-muted-foreground inline-flex items-center gap-1.5">
                    {step.result === "failed" ? (
                      <X className="size-3.5" aria-hidden />
                    ) : null}
                    <span
                      className={
                        step.result === "failed" ? "text-foreground" : undefined
                      }
                    >
                      {stepResultLabel(step.result)}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          </Section>

          <Section title="Reference">
            <span className="inline-flex items-center gap-1 border px-2 py-1 font-mono text-xs">
              {attempt.id}
              <CopyButton
                text={attempt.id}
                size="sm"
                tooltip="Copy reference"
              />
            </span>
          </Section>

          <ContextFacts attempt={attempt} providerName={providerName} />

          {attempt.person ? (
            <Section title="Related">
              <div className="divide-y border">
                <RelatedLink
                  to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(
                    {
                      ...(dayStart
                        ? { from: dayStart }
                        : { range: inLastDay ? "1d" : "30d" }),
                      people: [attempt.person.subjectUrn],
                      serverIds: attempt.server ? [attempt.server.id] : [],
                    },
                    now,
                  )}`}
                  title={`All outcomes for ${who}${gateway ? ` on ${gateway}` : ""}`}
                  detail={`${dayStart ? `Since ${formatDayMonth(attempt.startedAt)}` : inLastDay ? "Last 24 hours" : "Last 30 days"} · Connection Logs`}
                />
                <RelatedLink
                  to={`${routes.mcpSessions.provider.connections.href(providerId)}?subjectUrn=${encodeURIComponent(attempt.person.subjectUrn)}`}
                  title={`${who}'s sessions`}
                  detail="In Active Sessions"
                />
              </div>
            </Section>
          ) : null}
        </TabsContent>
        <TabsContent value="raw" className="mt-5">
          <CodeSnippet
            code={JSON.stringify(attempt, null, 2)}
            language="json"
            copyable
          />
        </TabsContent>
      </Tabs>

      <SheetFooter className="bg-card sticky bottom-0 border-t px-6 py-4">
        <div className="flex">
          <AskAiButton
            investigation={investigationFor(attempt)}
            onLaunched={onClose}
          />
        </div>
      </SheetFooter>
    </div>
  );
}

const SHEET_TAB = "flex-1 font-sans text-sm tracking-normal normal-case";

function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">{title}</h3>
      {children}
    </section>
  );
}

function ContextFacts({
  attempt,
  providerName,
}: {
  attempt: AttemptDetail;
  providerName?: string;
}): JSX.Element {
  const [open, setOpen] = useState(true);
  const facts: [string, ReactNode][] = [];
  if (attempt.source) {
    facts.push([
      "Source",
      attempt.source === "service" && attempt.service
        ? `${SOURCE_LABELS.service} (${attempt.service})`
        : SOURCE_LABELS[attempt.source],
    ]);
  }
  if (attempt.reason) {
    facts.push([
      "Reason code",
      <span key="reason" className="font-mono text-xs">
        {attempt.reason}
      </span>,
    ]);
  }
  if (attempt.server) facts.push(["Server", serverLabel(attempt)]);
  if (attempt.service) facts.push(["Service", attempt.service]);
  facts.push([
    "App",
    attempt.app.version
      ? appLabel(attempt.app)
      : `${attempt.app.name}. No version reported at this step.`,
  ]);
  if (providerName) facts.push(["Provider", providerName]);
  if (attempt.network) facts.push(["Network", attempt.network.label]);
  if (attempt.context.grantType)
    facts.push(["Grant", attempt.context.grantType]);
  if (attempt.context.retries > 0)
    facts.push(["Retries", String(attempt.context.retries)]);
  if (attempt.traceId) {
    facts.push([
      "Trace ID",
      <span key="trace" className="font-mono text-xs break-all">
        {attempt.traceId}
      </span>,
    ]);
  }
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1.5 text-sm">
        <ChevronDown
          className={`size-4 transition-transform ${open ? "" : "-rotate-90"}`}
          aria-hidden
        />
        Context
      </CollapsibleTrigger>
      <CollapsibleContent>
        <dl className="mt-2 grid grid-cols-[136px_1fr] gap-x-4 gap-y-1.5 text-sm">
          {facts.map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-eyebrow pt-0.5">{label}</dt>
              <dd className="min-w-0">{value}</dd>
            </div>
          ))}
        </dl>
      </CollapsibleContent>
    </Collapsible>
  );
}

function RelatedLink({
  to,
  title,
  detail,
}: {
  to: string;
  title: string;
  detail: string;
}): JSX.Element {
  return (
    <Link
      to={to}
      className="hover:bg-muted/40 flex items-center justify-between gap-3 px-4 py-3"
    >
      <div className="min-w-0">
        <div className="truncate text-sm">{title}</div>
        <div className="text-muted-foreground text-xs">{detail}</div>
      </div>
      <ArrowRight className="text-muted-foreground size-4 shrink-0" />
    </Link>
  );
}
