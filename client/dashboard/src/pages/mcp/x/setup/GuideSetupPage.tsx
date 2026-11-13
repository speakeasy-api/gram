import { PageEyebrow } from "@/components/page-eyebrow";
import { docsUrl, soleGuide } from "@/components/setup-guide/guideDocs";
import { SetupGuidePanel } from "@/components/setup-guide/SetupGuidePanel";
import { SetupGuideDoneButton } from "@/components/setup-guide/SetupGuideStep";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { formatRelativeTime } from "@/lib/dates";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";
import { Check, ExternalLink } from "lucide-react";
import { Link, Navigate } from "react-router";

/**
 * Setup for a provider with a written guide but no guided flow: the guide's
 * steps, full page, and a way to mark them done for the whole team.
 */
export function GuideSetupPage({
  serverUrl,
  mcpServer,
  overviewHref,
}: {
  serverUrl: string | undefined;
  mcpServer: McpServer;
  overviewHref: string;
}): JSX.Element {
  const docs = useGetMCPSetupDocs({ serverUrl }, undefined, {
    enabled: !!serverUrl,
    throwOnError: false,
  });
  if (!serverUrl) return <Navigate to={overviewHref} replace />;
  if (docs.isPending) return <Skeleton className="h-64 w-full" />;
  const guides = docs.data?.guides ?? [];
  if (guides.length === 0) return <Navigate to={overviewHref} replace />;
  const only = soleGuide(guides);
  const name = only?.title ?? mcpServer.name ?? "this server";
  const doneAt = mcpServer.setupGuideCompletedAt;

  return (
    // Inside the server's detail frame: a heading, then the guide beside a
    // status column that stays in view while the guide scrolls.
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-1 items-end gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(200px,3fr)] lg:gap-8 xl:gap-14">
        <div className="min-w-0">
          <PageEyebrow area={`${name} Setup`} className="mb-2" />
          <Heading
            variant="h2"
            className="text-[28px] leading-9 tracking-[-.03em]"
          >
            Set up {name}
          </Heading>
        </div>
        {only ? (
          <Button
            variant="secondary"
            size="sm"
            asChild
            className="bg-card mb-1 justify-self-start"
          >
            <a href={docsUrl(only)} target="_blank" rel="noopener noreferrer">
              <Button.Text>Open in Docs</Button.Text>
              <Button.RightIcon>
                <ExternalLink />
              </Button.RightIcon>
            </a>
          </Button>
        ) : null}
      </div>
      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(200px,3fr)] lg:gap-8 xl:gap-14">
        <section aria-label={`${name} setup guide`} className="min-w-0">
          <SetupGuidePanel serverUrl={serverUrl} className="pb-4" />
        </section>
        {/* Where the steps stand, beside them, so finishing the last one
            and marking it done are one glance apart. */}
        <aside className="flex flex-col gap-3 self-start lg:sticky lg:top-6">
          <span className="text-eyebrow">Status</span>
          {doneAt ? (
            <span className="flex items-center gap-2 text-sm">
              <Check className="size-4" aria-hidden />
              Marked done {formatRelativeTime(doneAt)}
            </span>
          ) : (
            <span className="text-muted-foreground text-sm">
              These steps happen in {name}. Mark them done when you have
              finished, so your team knows.
            </span>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <SetupGuideDoneButton serverId={mcpServer.id} done={!!doneAt} />
            {doneAt ? (
              <Button variant="secondary" size="sm" asChild>
                <Link to={overviewHref}>
                  <Button.Text>Back to overview</Button.Text>
                </Link>
              </Button>
            ) : null}
          </div>
        </aside>
      </div>
    </div>
  );
}
