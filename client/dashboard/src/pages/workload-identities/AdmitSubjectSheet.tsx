import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { TagInput } from "@/components/ui/TagInput";
import { Text } from "@/components/ui/Text";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { useState } from "react";
import { admissionValuesDiffer } from "./admissionEdit";
import {
  buildAdmitValues,
  canAdmit,
  inferMatchKind,
  type MatchKind,
  subjectRuleWarning,
} from "./subjectRule";
import { tagsProblem } from "./tagLimits";

export interface AdmitSubjectValues {
  issuer: string;
  subject: string;
  matchKind: MatchKind;
  name: string;
  tags: string[];
  agentId: string;
}

/**
 * The form's own state. matchKind is derived and the issuer comes from the page,
 * so neither is held here.
 */
export interface AdmitSubjectFormValues {
  subject: string;
  name: string;
  tags: string[];
  agentId: string;
}

/** An allowed machine's current values, for editing it. */
export interface AdmitSubjectInitialValues extends AdmitSubjectFormValues {
  /**
   * The assigned agent's name, so the picker can show it even when the agent
   * has dropped out of the active list.
   */
  agentName: string;
}

interface AdmitSubjectSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Called with the submitted values and, when editing, the machine's values as
   * they stood when the sheet opened, which is what the edit is diffed against.
   */
  onSubmit: (
    values: AdmitSubjectValues,
    baseline: AdmitSubjectInitialValues | undefined,
  ) => void;
  isPending: boolean;
  /** The platform whose page opened the sheet; every machine allowed here is admitted under it. */
  issuer: WorkloadIssuer;
  agents: { id: string; name: string }[];
  /**
   * An allowed machine's current values. When set, the sheet edits that
   * machine: every time it opens the form is prefilled from them, and the
   * subject is read-only, because the server fixes it, and its match kind, at
   * admission.
   */
  initial?: AdmitSubjectInitialValues;
}

const EMPTY: AdmitSubjectFormValues = {
  subject: "",
  name: "",
  tags: [],
  agentId: "",
};

function formValues(
  initial: AdmitSubjectInitialValues | undefined,
): AdmitSubjectFormValues {
  if (initial === undefined) {
    return EMPTY;
  }
  return {
    subject: initial.subject,
    name: initial.name,
    tags: initial.tags,
    agentId: initial.agentId,
  };
}

/**
 * The agents to offer. An edit keeps the machine's assigned agent in the list
 * even once it is no longer active, so the picker shows what is assigned
 * rather than appearing empty.
 */
function withAssignedAgent(
  agents: { id: string; name: string }[],
  initial: AdmitSubjectInitialValues | undefined,
): { id: string; name: string }[] {
  if (
    initial === undefined ||
    initial.agentId.length === 0 ||
    agents.some((agent) => agent.id === initial.agentId)
  ) {
    return agents;
  }
  return [
    { id: initial.agentId, name: initial.agentName || "Assigned agent" },
    ...agents,
  ];
}

function submitLabel(isEditing: boolean, isPending: boolean): string {
  if (isEditing) {
    return isPending ? "Saving…" : "Save changes";
  }
  return isPending ? "Allowing…" : "Allow access";
}

function SubjectHint({ isEditing }: { isEditing: boolean }): JSX.Element {
  if (isEditing) {
    return (
      <Text muted small>
        Fixed once access is allowed. To allow a different subject, allow access
        for it separately.
      </Text>
    );
  }
  return (
    <Text muted small>
      Surrounding spaces are trimmed; otherwise stored and compared exactly as
      entered. End it with <code>*</code> to admit every subject beginning with
      the part before the <code>*</code>.
    </Text>
  );
}

function AgentHint({ isEditing }: { isEditing: boolean }): JSX.Element {
  if (isEditing) {
    return (
      <Text muted small>
        The machine inherits this agent&apos;s policy in full. Where the same
        subject is also allowed at the other tier, that access uses the same
        agent and changes with it.
      </Text>
    );
  }
  return (
    <Text muted small>
      Choose the most narrowly scoped agent that can do the job. The machine
      inherits its policy in full.
    </Text>
  );
}

export function AdmitSubjectSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  issuer,
  agents,
  initial,
}: AdmitSubjectSheetProps): JSX.Element {
  const isEditing = initial !== undefined;
  const [values, setValues] = useState<AdmitSubjectFormValues>(() =>
    formValues(initial),
  );
  // The machine as it stood when the sheet opened. A query refresh can replace
  // `initial` mid-edit, and diffing against that would turn fields the operator
  // never touched into changes that overwrite the newer values.
  const [baseline, setBaseline] = useState(initial);

  // The sheet stays mounted between uses, so each opening starts the form over
  // from what is stored now. It is done as the sheet opens, during render,
  // because the page hands over a machine's values in the same render that
  // opens the sheet: a reset on close would keep what the form held before
  // them, and the next save would write those stale values back.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setValues(formValues(initial));
      setBaseline(initial);
    }
  }

  const wildcardAvailable = issuer.allowWildcardAdmission;

  // The terminator is how a rule states its own breadth, so the kind is read off
  // the value and a separate control could only disagree with it.
  const matchKind = inferMatchKind(values.subject);
  const storedSubject = values.subject.trim();
  // An issuer can forbid wildcards (an older row, or one cleared during an
  // incident). Say so under the field rather than leave the submit disabled
  // with no reason.
  const admitWarning =
    subjectRuleWarning(matchKind, storedSubject) ??
    (matchKind === "wildcard" && !wildcardAvailable
      ? "This issuer does not permit wildcard matching, so a rule ending in \u201c*\u201d cannot be admitted under it. Give the subject in full, or turn wildcard admission back on for the issuer."
      : null);
  // The subject is fixed once admitted, so an edit has nothing to warn about.
  const warning = isEditing ? null : admitWarning;

  // Naming the stem and the agent makes the reach of a wildcard rule concrete.
  const wildcardStem =
    matchKind === "wildcard" && storedSubject.endsWith("*")
      ? storedSubject.slice(0, -1)
      : "";
  const agentOptions = withAssignedAgent(agents, baseline);
  const selectedAgentName =
    agentOptions.find((agent) => agent.id === values.agentId)?.name ?? "";
  const showCaution = wildcardStem.length > 0 && warning === null;

  const tagProblem = tagsProblem(values.tags);

  // The agent list refetches while the sheet is open, and the server accepts a
  // suspended or revoked agent — leaving a machine that authenticates and can
  // reach nothing — so a choice that has dropped out of the active list is
  // refused here. An edit that leaves the agent alone does not send it, so the
  // assigned agent needs no such check.
  const agentStillActive =
    agents.some((agent) => agent.id === values.agentId) ||
    (baseline !== undefined && values.agentId === baseline.agentId);

  const hasChanges =
    baseline === undefined || admissionValuesDiffer(baseline, values);

  const canSubmit =
    hasChanges &&
    tagProblem === null &&
    agentStillActive &&
    (isEditing ||
      canAdmit({
        subject: storedSubject,
        agentId: values.agentId,
        warning,
        matchKindPermitted: matchKind !== "wildcard" || wildcardAvailable,
      }));

  const handleSubmit: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!canSubmit || isPending) return;
    onSubmit(buildAdmitValues(values, issuer.issuer), baseline);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            {isEditing ? "Edit access" : "Allow access"}
          </SheetTitle>
          <SheetDescription>
            {isEditing
              ? "The subject is fixed once access is allowed. Changing the agent changes the whole policy this machine acts under."
              : "Allowing access lets a machine sign in to Gram. The agent you choose supplies the whole policy that the machine acts under, so it is chosen at the same time."}
          </SheetDescription>
        </SheetHeader>

        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
          <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
            <Stack gap={2}>
              <Label htmlFor="admit-subject">Subject</Label>
              <Input
                id="admit-subject"
                value={values.subject}
                placeholder="wimse://identity.example.com/org/acme/agent/a-1"
                readOnly={isEditing}
                aria-describedby={warning ? "admit-subject-warning" : undefined}
                aria-invalid={warning !== null}
                onChange={(value) => setValues({ ...values, subject: value })}
              />
              {warning !== null ? (
                <Text id="admit-subject-warning" role="alert" small destructive>
                  {warning}
                </Text>
              ) : (
                <SubjectHint isEditing={isEditing} />
              )}
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="admit-agent">Agent</Label>
              <Select
                value={values.agentId}
                onValueChange={(agentId) => setValues({ ...values, agentId })}
              >
                <SelectTrigger id="admit-agent" className="w-full">
                  <SelectValue placeholder="Select an agent" />
                </SelectTrigger>
                <SelectContent>
                  {agentOptions.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <AgentHint isEditing={isEditing} />
              {showCaution && (
                // alignTop because this body runs to several lines: a centred icon
                // drifts into the middle of the text and stops reading as a marker.
                <Alert variant="warning" alignTop>
                  {/* Plain elements so the copy takes the Alert's warning
                      color, which Text's own color class would override. */}
                  <div className="text-sm break-words">
                    <p className="font-medium">
                      This rule admits more than one identity
                    </p>
                    <p>
                      Any subject beginning{" "}
                      {/* A subject is one unbroken token, so it has to be told it
                        may wrap: <code> will not on its own, and the sheet
                        would scroll sideways instead for a realistic issuer
                        URL. */}
                      <code className="break-all">{wildcardStem}</code>
                      {selectedAgentName
                        ? ` is admitted, and each will inherit ${selectedAgentName}'s policy. `
                        : " is admitted. "}
                      Check that the varying part is assigned by the issuer
                      rather than chosen by the caller — where a caller can
                      influence it, this admits anyone who can.
                    </p>
                  </div>
                </Alert>
              )}
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="admit-name">Label (optional)</Label>
              <Input
                id="admit-name"
                value={values.name}
                placeholder="Deploy bot"
                onChange={(value) => setValues({ ...values, name: value })}
              />
              <Text muted small>
                Useful where a subject is opaque, such as a numeric service
                account id.
              </Text>
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="admit-tags">Tags</Label>
              <TagInput
                id="admit-tags"
                value={values.tags}
                placeholder="support, production"
                error={tagProblem !== null}
                ariaDescribedBy={
                  tagProblem !== null ? "admit-tags-error" : undefined
                }
                onChange={(tags) => setValues({ ...values, tags })}
              />
              {tagProblem !== null ? (
                <Text id="admit-tags-error" role="alert" small destructive>
                  {tagProblem}
                </Text>
              ) : (
                <Text muted small>
                  Optional labels for finding this machine later. Not used for
                  matching.
                </Text>
              )}
            </Stack>
          </div>

          <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
              disabled={isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              type="submit"
              variant="primary"
              disabled={!canSubmit || isPending}
            >
              <Button.Text>{submitLabel(isEditing, isPending)}</Button.Text>
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
