import type { AdminOnboardingConfiguration } from "@gram/admin-client/models/components/adminonboardingconfiguration";

export function groupOnboardingTasks(
  tasks: AdminOnboardingConfiguration["tasks"],
  workstreams: AdminOnboardingConfiguration["workstreams"],
): {
  id: string;
  title: string;
  tasks: AdminOnboardingConfiguration["tasks"];
}[] {
  const byKey = new Map(tasks.map((task) => [task.key, task]));
  const knownKeys = new Set(workstreams.flatMap((group) => group.taskKeys));
  const groups = workstreams.map((group) => ({
    id: group.id,
    title: group.title,
    tasks: group.taskKeys
      .map((key) => byKey.get(key))
      .filter((task) => task !== undefined),
  }));
  // Tasks outside the catalog remain editable in response order.
  groups.push({
    id: "other",
    title: "Other tasks",
    tasks: tasks.filter((task) => !knownKeys.has(task.key)),
  });
  return groups.filter((group) => group.tasks.length > 0);
}
