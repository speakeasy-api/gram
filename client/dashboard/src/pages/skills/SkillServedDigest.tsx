import { CopyButton } from "@/components/ui/CopyButton";
import { cn } from "@/lib/utils";
import { servedSkillDigest, shortServedSkillDigest } from "./mcp-served-skills";

/**
 * The digest an agent sees for a skill version when it loads the skill over
 * MCP: a shortened `sha256:…` label with the full value on hover and a copy
 * button, so a digest in an agent's log can be matched to a version here.
 */
export function SkillServedDigest({
  rawSha256,
  className,
}: {
  rawSha256: string;
  className?: string;
}): JSX.Element {
  const digest = servedSkillDigest(rawSha256);
  return (
    <span
      className={cn(
        "text-muted-foreground inline-flex min-w-0 items-center gap-1",
        className,
      )}
    >
      <span className="truncate font-mono text-xs" title={digest}>
        {shortServedSkillDigest(rawSha256)}
      </span>
      <CopyButton
        text={digest}
        size="xs"
        tooltip="Copy MCP digest"
        className="shrink-0"
      />
    </span>
  );
}
