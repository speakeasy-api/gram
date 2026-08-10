import { GramLogo } from "@/components/gram-logo";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { Markdown } from "@/elements/components/Markdown";
import { dateTimeFormatters } from "@/lib/dates";
import type { SharedSkill2 } from "@gram/client/models/components/sharedskill2.js";
import { useSharedSkill } from "@gram/client/react-query/sharedSkill.js";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useParams } from "react-router";
import { toast } from "sonner";
import { stripSkillFrontmatter } from "./skill-manifest";

/**
 * SharedSkillPage is the standalone public skill page served at
 * /shared/skills/:token, deliberately rendered OUTSIDE the dashboard shell
 * (no sidebar / header) and outside LoginCheck: the share token is the only
 * credential, so anonymous visitors can read the latest version of a shared
 * skill without signing in.
 */
export function SharedSkillPage(): JSX.Element {
  const { token } = useParams<{ token: string }>();

  return (
    <div className="bg-background flex min-h-screen w-full flex-col">
      <main className="mx-auto w-full max-w-3xl flex-1 px-6 py-12">
        <SharedSkillBody token={token} />
      </main>
      <footer className="flex items-center justify-center gap-2 pb-8">
        <Text muted small>
          Powered by
        </Text>
        <GramLogo className="w-16" />
      </footer>
    </div>
  );
}

function SharedSkillBody({ token }: { token: string | undefined }) {
  const query = useSharedSkill(
    { token: token ?? "" },
    {
      enabled: !!token,
      retry: false,
      refetchOnWindowFocus: false,
      // The QueryClient default rethrows non-403 errors to the nearest error
      // boundary, which for this standalone page is the full-page crash
      // screen. A dead or mistyped link is an expected state here — handle
      // it inline with the friendly unavailable panel instead.
      throwOnError: false,
    },
  );

  if (!token) {
    return <SharedSkillUnavailable />;
  }
  if (query.isPending) {
    return (
      <Stack
        direction="horizontal"
        gap={2}
        align="center"
        className="justify-center py-24"
      >
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Text muted small>
          Loading skill…
        </Text>
      </Stack>
    );
  }
  if (query.error || !query.data) {
    return <SharedSkillUnavailable />;
  }

  return <SharedSkillDocument skill={query.data.result} />;
}

function SharedSkillDocument({ skill }: { skill: SharedSkill2 }): JSX.Element {
  const body = stripSkillFrontmatter(skill.content);

  const copyMarkdown = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(skill.content);
      toast.success("Markdown copied");
    } catch {
      toast.error("Unable to copy markdown");
    }
  };

  return (
    <article className="space-y-8">
      <header className="space-y-3">
        <h1 className="font-display text-3xl font-thin">{skill.displayName}</h1>
        {skill.summary && (
          <Text muted className="max-w-2xl">
            {skill.summary}
          </Text>
        )}
        <Text small muted className="block">
          Updated {dateTimeFormatters.full.format(skill.updatedAt)}
        </Text>
        <div className="flex flex-wrap items-center gap-2 pt-1">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void copyMarkdown()}
          >
            Copy markdown
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => downloadSkillMarkdown(skill.content)}
          >
            Download SKILL.md
          </Button>
        </div>
      </header>
      <div className="border-t pt-8">
        {body.trim().length === 0 ? (
          <Text small muted>
            This manifest has no Markdown body.
          </Text>
        ) : (
          <Markdown>{body}</Markdown>
        )}
      </div>
    </article>
  );
}

function SharedSkillUnavailable(): JSX.Element {
  return (
    <Stack gap={3} align="center" className="py-24">
      <Icon name="link-2-off" className="text-muted-foreground size-8" />
      <Text variant="subheading" className="text-center">
        This skill isn't available
      </Text>
      <Text muted small className="max-w-md text-center">
        The link may have been turned off by its owner, or the address might not
        be quite right. Ask whoever shared it with you for a fresh link.
      </Text>
    </Stack>
  );
}

function downloadSkillMarkdown(content: string): void {
  const blob = new Blob([content], { type: "text/markdown" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "SKILL.md";
  anchor.click();
  URL.revokeObjectURL(url);
}
