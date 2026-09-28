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
import { useEffect, useState } from "react";
import {
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
interface AdmitSubjectForm {
  subject: string;
  name: string;
  tags: string[];
  agentId: string;
}

interface AdmitSubjectSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: AdmitSubjectValues) => void;
  isPending: boolean;
  /** The platform whose page opened the sheet; every machine allowed here is admitted under it. */
  issuer: WorkloadIssuer;
  agents: { id: string; name: string }[];
}

const EMPTY: AdmitSubjectForm = {
  subject: "",
  name: "",
  tags: [],
  agentId: "",
};

export function AdmitSubjectSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  issuer,
  agents,
}: AdmitSubjectSheetProps): JSX.Element {
  const [values, setValues] = useState<AdmitSubjectForm>(EMPTY);

  // A successful admission closes the sheet through the parent's own state,
  // which never reaches handleOpenChange — so without this the next admission
  // opens prefilled with the previous workload. The sheet stays mounted, so
  // there is no unmount to do it for us.
  useEffect(() => {
    if (!open) {
      setValues(EMPTY);
    }
  }, [open]);

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setValues(EMPTY);
    }
    onOpenChange(next);
  };

  // The wildcard option is offered only where the issuer permits it, rather than
  // offered everywhere and refused on submit.
  const wildcardAvailable = issuer.allowWildcardAdmission;

  // The terminator belongs to the match kind, not to the text: the field holds
  // the stem and the "*" is rendered after it, so selecting Wildcard is the only
  // place breadth is stated. Validate and submit the composed value, which is
  // what the row will actually hold.
  // Read off the value rather than selected: the terminator is how a rule states
  // its own breadth, so a separate control would only be a second way to say the
  // same thing — and a way for the two to disagree.
  const matchKind = inferMatchKind(values.subject);
  const storedSubject = values.subject.trim();
  // The issuer's permission is a state the operator can reach — an older row, or
  // one cleared during an incident — and with the match control gone the field is
  // the only place left to say so. Without this the submit would sit disabled
  // with no reason, which is the failure the caution below exists to avoid.
  const warning =
    subjectRuleWarning(matchKind, storedSubject) ??
    (matchKind === "wildcard" && !wildcardAvailable
      ? "This issuer does not permit wildcard matching, so a rule ending in \u201c*\u201d cannot be admitted under it. Give the subject in full, or turn wildcard admission back on for the issuer."
      : null);

  // Stated where the rule is written rather than asked when the issuer is
  // registered. An operator registering an issuer has no rule in mind yet, and
  // whether a wildcard is sound is a judgement about their own platform. Naming
  // the stem and the agent makes the reach of the rule concrete.
  const wildcardStem =
    matchKind === "wildcard" && storedSubject.endsWith("*")
      ? storedSubject.slice(0, -1)
      : "";
  const selectedAgentName =
    agents.find((agent) => agent.id === values.agentId)?.name ?? "";
  const showCaution = wildcardStem.length > 0 && warning === null;

  const tagProblem = tagsProblem(values.tags);

  const canSubmit =
    tagProblem === null &&
    canAdmit({
      subject: storedSubject,
      agentId: values.agentId,
      warning,
      matchKindPermitted: matchKind !== "wildcard" || wildcardAvailable,
    });

  const handleSubmit: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!canSubmit || isPending) return;
    onSubmit({
      ...values,
      issuer: issuer.issuer,
      subject: storedSubject,
      matchKind,
    });
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Allow a machine
          </SheetTitle>
          <SheetDescription>
            Allowing a machine is the grant of access. The agent you choose
            supplies the whole policy that machine acts under, so it is chosen
            at the same time.
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
                aria-describedby={warning ? "admit-subject-warning" : undefined}
                aria-invalid={warning !== null}
                onChange={(value) => setValues({ ...values, subject: value })}
              />
              {warning !== null ? (
                <Text id="admit-subject-warning" role="alert" small destructive>
                  {warning}
                </Text>
              ) : (
                <Text muted small>
                  Stored and compared exactly as entered, and not normalized.
                  End it with <code>*</code> to admit every subject beginning
                  with the part before the <code>*</code>.
                </Text>
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
                  {agents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Text muted small>
                Choose the most narrowly scoped agent that can do the job. The
                machine inherits its policy in full.
              </Text>
              {showCaution && (
                // alignTop because this body runs to several lines: a centred icon
                // drifts into the middle of the text and stops reading as a marker.
                <Alert variant="warning" alignTop>
                  <div role="status" className="break-words">
                    <Text small className="font-medium">
                      This rule admits more than one identity
                    </Text>
                    <Text small>
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
                    </Text>
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
              onClick={() => handleOpenChange(false)}
              disabled={isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              type="submit"
              variant="primary"
              disabled={!canSubmit || isPending}
            >
              <Button.Text>
                {isPending ? "Allowing…" : "Allow machine"}
              </Button.Text>
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
