import { Severity } from "@gram/client/models/operations/listmcpfeedback.js";

/** The impact an agent can give a report, worst first. */
export type FeedbackSeverity = (typeof Severity)[keyof typeof Severity];

export const FEEDBACK_SEVERITIES: readonly {
  value: FeedbackSeverity;
  label: string;
  /** What the agent meant by picking it, in the words its tool gives it. */
  meaning: string;
  badge: "destructive" | "warning" | "neutral";
}[] = [
  {
    value: Severity.Blocked,
    label: "Blocked",
    meaning: "The agent could not finish its task",
    badge: "destructive",
  },
  {
    value: Severity.Degraded,
    label: "Degraded",
    meaning: "The agent finished, by a slower or awkward path",
    badge: "warning",
  },
  {
    value: Severity.Cosmetic,
    label: "Cosmetic",
    meaning: "It worked, but confused the agent",
    badge: "neutral",
  },
];

/**
 * The severity a report carries, when it is one this page knows. A report with
 * none is a general comment; one this page does not know yet is shown by its
 * raw value where it is displayed.
 */
export function feedbackSeverity(
  value: string | undefined,
): (typeof FEEDBACK_SEVERITIES)[number] | undefined {
  return FEEDBACK_SEVERITIES.find((severity) => severity.value === value);
}
