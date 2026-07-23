import { GramLogo } from "@/components/gram-logo";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { Markdown } from "@/elements/components/Markdown";
import { dateTimeFormatters } from "@/lib/dates";
import type { SharedSkill2 } from "@gram/client/models/components/sharedskill2.js";
import { useSharedSkill } from "@gram/client/react-query/sharedSkill.js";
import { Icon, Stack } from "@speakeasy-api/moonshine";
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
        <Type muted small>
          Powered by
        </Type>
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
        <Type muted small>
          Loading skill…
        </Type>
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
        <h1 className="text-3xl font-semibold">{skill.displayName}</h1>
        {skill.summary && (
          <Type muted className="max-w-2xl">
            {skill.summary}
          </Type>
        )}
        <Type small muted className="block">
          Updated {dateTimeFormatters.full.format(skill.updatedAt)}
        </Type>
        <div className="flex flex-wrap items-center gap-2 pt-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => void copyMarkdown()}
          >
            Copy markdown
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => downloadSkillMarkdown(skill.content)}
          >
            Download SKILL.md
          </Button>
        </div>
      </header>
      <div className="border-t pt-8">
        {body.trim().length === 0 ? (
          <Type small muted>
            This manifest has no Markdown body.
          </Type>
        ) : (
          <Markdown>{body}</Markdown>
        )}
      </div>
    </article>
  );
}

function SharedSkillUnavailable(): JSX.Element {
  return (
    <Stack gap={2} align="center" className="py-24">
      <Type variant="subheading" className="text-center">
        This skill isn't available
      </Type>
      <Type muted small className="max-w-md text-center">
        The link may have been turned off or the address is wrong.
      </Type>
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
