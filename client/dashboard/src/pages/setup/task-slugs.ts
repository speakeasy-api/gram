import {
  ONBOARDING_TASK_IDS,
  ONBOARDING_TASKS,
  isOnboardingTaskId,
  type OnboardingTaskPresentation,
} from "./onboarding-tasks";

// Legacy /{org}/setup/{slug} aliases normalize to the shared ?task=<key> entry.
export function setupTaskSlug(taskKey: string): string {
  if (!isOnboardingTaskId(taskKey)) return taskKey;
  const task: OnboardingTaskPresentation = ONBOARDING_TASKS[taskKey];
  return task.slug ?? taskKey;
}

export function setupTaskKeyForSlug(slug: string): string | undefined {
  // Task keys still work as slugs, so links minted with a key keep resolving.
  return ONBOARDING_TASK_IDS.find(
    (key) => setupTaskSlug(key) === slug || key === slug,
  );
}

export function canonicalSetupSearch(
  search: URLSearchParams,
  taskSlug?: string,
): URLSearchParams {
  const next = new URLSearchParams(search);
  const step = next.get("step");
  const legacyTask = step && isOnboardingTaskId(step) ? step : undefined;
  if (!next.has("task")) {
    const task = taskSlug
      ? (setupTaskKeyForSlug(taskSlug) ?? taskSlug)
      : legacyTask;
    if (task) next.set("task", task);
    if (legacyTask && !taskSlug) next.delete("step");
  }
  return next;
}
