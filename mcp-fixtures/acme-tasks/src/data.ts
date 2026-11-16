// Fixed sample data. The tools never call out or persist anything, so every
// call returns the same answer and a reviewer or a canary can assert on it.

export type TaskStatus = "todo" | "in_progress" | "done";

export type Task = {
  id: string;
  title: string;
  status: TaskStatus;
  assignee: string;
  due: string;
  labels: string[];
};

export const tasks: readonly Task[] = [
  {
    id: "ACME-101",
    title: "Draft the Q4 onboarding checklist",
    status: "in_progress",
    assignee: "Riley Chen",
    due: "2026-11-07",
    labels: ["onboarding"],
  },
  {
    id: "ACME-102",
    title: "Rotate the staging database credentials",
    status: "todo",
    assignee: "Sam Okafor",
    due: "2026-11-03",
    labels: ["security", "ops"],
  },
  {
    id: "ACME-103",
    title: "Publish the October release notes",
    status: "done",
    assignee: "Riley Chen",
    due: "2026-10-31",
    labels: ["docs"],
  },
  {
    id: "ACME-104",
    title: "Fix the flaky checkout integration test",
    status: "in_progress",
    assignee: "Priya Nair",
    due: "2026-11-05",
    labels: ["bug", "ci"],
  },
  {
    id: "ACME-105",
    title: "Review the vendor security questionnaire",
    status: "todo",
    assignee: "Sam Okafor",
    due: "2026-11-12",
    labels: ["security"],
  },
  {
    id: "ACME-106",
    title: "Plan the customer advisory board agenda",
    status: "todo",
    assignee: "Priya Nair",
    due: "2026-11-19",
    labels: ["customers"],
  },
];
