import type { ReactNode } from "react";
import type { BoardTask } from "./board-store";
import type { OnboardingWorkstreamDefinition } from "./tasks";

interface WorkstreamColumnProps {
  workstream: OnboardingWorkstreamDefinition;
  tasks: BoardTask[];
  children: ReactNode;
}

export function WorkstreamColumn({
  workstream,
  tasks,
  children,
}: WorkstreamColumnProps): JSX.Element {
  const requiredTasks = tasks.filter((task) => task.id !== "platform-mcp");
  const completedTasks = requiredTasks.filter(
    (task) => task.status === "done",
  ).length;
  const headingId = `onboarding-workstream-${workstream.id}`;

  return (
    <section
      aria-labelledby={headingId}
      className="bg-card flex min-h-0 flex-col border"
    >
      <header className="bg-surface-secondary-default flex items-start justify-between gap-4 border-b px-4 py-4">
        <div className="min-w-0">
          <h2 id={headingId} className="text-foreground font-medium">
            {workstream.title}
          </h2>
          <p className="text-muted-foreground mt-1 text-sm">
            {workstream.description}
          </p>
        </div>
        <p className="text-muted-foreground shrink-0 text-xs font-medium tabular-nums">
          {completedTasks} / {requiredTasks.length}
        </p>
      </header>
      <div className="grid flex-1 content-start gap-3 p-3">{children}</div>
    </section>
  );
}
