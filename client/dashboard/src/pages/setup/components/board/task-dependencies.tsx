import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import type { OnboardingTaskId } from "./tasks";

// Dependency payloads contain keys rather than the task titles shown on the board.
const DEPENDENCY_TITLES: Record<OnboardingTaskId, string> = {
  "identity-provider": "Set up identity provider",
  "connect-idp": "Connect identity provider",
  "directory-sync": "Set up directory sync",
  "enable-logging": "Enable logging",
  "anthropic-observability": "Set up Anthropic observability",
  "anthropic-admin-controls": "Set up Anthropic admin controls",
  "create-marketplace": "Create marketplace",
  "instrument-agents": "Set up observability in other platforms",
  litellm: "Set up LiteLLM",
  "additional-agent-config": "Configure integrations",
  "confirm-traffic": "Confirm traffic",
  "distribute-servers": "Distribute MCP servers",
  "configure-policies": "Configure policies",
  "platform-mcp": "Set up Platform MCP",
};

function dependencyTitle(key: string): string {
  if (Object.hasOwn(DEPENDENCY_TITLES, key)) {
    return DEPENDENCY_TITLES[key as OnboardingTaskId];
  }
  const words = key.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

export function TaskDependencies({
  dependencies,
  wrapTitles = false,
}: {
  dependencies: string[];
  wrapTitles?: boolean;
}): JSX.Element | null {
  if (dependencies.length === 0) return null;

  return (
    <div className="text-default-warning flex min-w-0 items-start gap-1.5 text-xs leading-snug">
      <span aria-hidden="true" className="mt-1.5 shrink-0">
        <Icon name="link-2" className="size-3" />
      </span>
      <div className="flex min-w-0 flex-1 items-start gap-1.5">
        <span className="shrink-0 py-1">Requires</span>
        <ul
          role="list"
          aria-label="Prerequisite tasks"
          className="flex min-w-0 flex-1 flex-col items-start gap-1"
        >
          {dependencies.map((dependency) => (
            <li
              key={dependency}
              title={dependencyTitle(dependency)}
              className={cn(
                "bg-warning-softest border-warning-softest max-w-full rounded-sm border px-2 py-1",
                wrapTitles ? "whitespace-normal" : "truncate",
              )}
            >
              {dependencyTitle(dependency)}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
