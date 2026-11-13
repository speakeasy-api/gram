import { PageEyebrow } from "@/components/page-eyebrow";
import { Page } from "@/components/page-layout";
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
import { SetupHeader } from "./SetupHeader";

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
  if (docs.isPending)
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Skeleton className="h-64 w-full" />
        </Page.Body>
      </Page>
    );
  const guides = docs.data?.guides ?? [];
  if (guides.length === 0) return <Navigate to={overviewHref} replace />;
  const only = soleGuide(guides);
  const name = only?.title ?? mcpServer.name ?? "this server";
  const doneAt = mcpServer.setupGuideCompletedAt;

  return (
    <Page>
      <Page.Header>{null}</Page.Header>
      <Page.Body
        fullWidth
        noPadding
        className="bg-card min-h-[calc(100vh-var(--page-sticky-top,64px))] gap-0"
      >
        <SetupHeader dither={!doneAt}>
          <div className="mx-auto w-full max-w-[1234px] px-5 pt-18 pb-4 md:px-7 md:pt-20 md:pb-5 xl:px-10">
            <div className="grid grid-cols-1 items-end gap-6 lg:grid-cols-[minmax(0,768px)_minmax(170px,1fr)] lg:gap-8 xl:gap-14">
              <div className="min-w-0">
                <PageEyebrow area={`${name} Setup`} className="mb-2" />
                <Heading
                  variant="h1"
                  className="text-[38px] leading-[1.375] tracking-[-.04em]"
                >
                  Set up {name}
                </Heading>
              </div>
              {only ? (
                <Button
                  variant="secondary"
                  size="sm"
                  asChild
                  className="bg-card mb-2 justify-self-start"
                >
                  <a
                    href={docsUrl(only)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <Button.Text>Open in Docs</Button.Text>
                    <Button.RightIcon>
                      <ExternalLink />
                    </Button.RightIcon>
                  </a>
                </Button>
              ) : null}
            </div>
          </div>
          <div
            aria-hidden
            className="h-px bg-[linear-gradient(90deg,#320f1e,#c83228,#fb873f,#d2dc91,#5a8250,#002314,#00143c,#2873d7,#9bc3ff)]"
          />
        </SetupHeader>
        <div className="mx-auto w-full max-w-[1234px] flex-1 px-5 pt-4 pb-24 md:px-7 lg:pt-6 xl:px-10 xl:pt-8">
          <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,768px)_minmax(170px,1fr)] lg:gap-8 xl:gap-14">
            <section
              aria-label={`${name} setup guide`}
              className="min-w-0 max-w-[768px]"
            >
              <SetupGuidePanel serverUrl={serverUrl} className="pb-4" />
            </section>
            {/* Where the steps stand, beside them, so finishing the last one
                and marking it done are one glance apart. */}
            <aside className="flex flex-col gap-3 lg:sticky lg:top-8">
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
      </Page.Body>
    </Page>
  );
}
