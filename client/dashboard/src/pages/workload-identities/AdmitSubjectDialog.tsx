import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { useEffect, useMemo, useState } from "react";
import {
  canAdmit,
  inferMatchKind,
  type MatchKind,
  subjectRuleWarning,
} from "./subjectRule";

export interface AdmitSubjectValues {
  issuer: string;
  subject: string;
  matchKind: MatchKind;
  name: string;
  agentId: string;
}

/** The form's own state. matchKind is derived, so it is not held here. */
interface AdmitSubjectForm {
  issuer: string;
  subject: string;
  name: string;
  agentId: string;
}

interface AdmitSubjectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: AdmitSubjectValues) => void;
  isPending: boolean;
  issuers: WorkloadIssuer[];
  agents: { id: string; name: string }[];
}

const EMPTY: AdmitSubjectForm = {
  issuer: "",
  subject: "",
  name: "",
  agentId: "",
};

export function AdmitSubjectDialog({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  issuers,
  agents,
}: AdmitSubjectDialogProps): JSX.Element {
  const [values, setValues] = useState<AdmitSubjectForm>(EMPTY);

  // A successful admission closes the dialog through the parent's own state,
  // which never reaches handleOpenChange — so without this the next admission
  // opens prefilled with the previous workload. The dialog stays mounted, so
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

  // Derived during render rather than synced in an effect: a single trusted
  // issuer is preselected, which is the normal shape while an organization is
  // federating its first platform and saves a pointless choice of one.
  const selectedIssuerUrl =
    values.issuer.length > 0
      ? values.issuer
      : issuers.length === 1
        ? issuers[0]!.issuer
        : "";

  const selectedIssuer = useMemo(
    () => issuers.find((issuer) => issuer.issuer === selectedIssuerUrl),
    [issuers, selectedIssuerUrl],
  );

  // The wildcard option is offered only where the issuer permits it, rather than
  // offered everywhere and refused on submit.
  const wildcardAvailable = selectedIssuer?.allowWildcardAdmission ?? false;

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
    (matchKind === "wildcard" &&
    !wildcardAvailable &&
    selectedIssuerUrl.length > 0
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

  const canSubmit = canAdmit({
    issuer: selectedIssuerUrl,
    subject: storedSubject,
    agentId: values.agentId,
    warning,
    issuerExists: selectedIssuer !== undefined,
    matchKindPermitted: matchKind !== "wildcard" || wildcardAvailable,
  });

  const handleIssuerChange = (issuer: string) => {
    // No kind to carry across: it is read off the subject, so switching to an
    // issuer that forbids wildcards leaves the rule as written and canAdmit
    // refuses it, with the reason shown under the field.
    setValues({ ...values, issuer });
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Admit a workload</Dialog.Title>
          <Dialog.Description>
            Admitting a subject is the grant of machine access. The agent you
            choose supplies the whole policy the workload acts under, so it is
            assigned at the same time.
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={4}>
          <Stack gap={2}>
            <Label htmlFor="admit-issuer">Issuer</Label>
            <Select
              value={selectedIssuerUrl}
              onValueChange={handleIssuerChange}
            >
              <SelectTrigger id="admit-issuer">
                <SelectValue placeholder="Select a trusted issuer" />
              </SelectTrigger>
              <SelectContent>
                {issuers.map((issuer) => (
                  <SelectItem key={issuer.id} value={issuer.issuer}>
                    {issuer.name} — {issuer.issuer}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Stack>

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
                Stored and compared exactly as entered, and not normalized. End
                it with <code>*</code> to admit every subject beginning with the
                part before the <code>*</code>.
              </Text>
            )}
          </Stack>

          <Stack gap={2}>
            <Label htmlFor="admit-agent">Agent</Label>
            <Select
              value={values.agentId}
              onValueChange={(agentId) => setValues({ ...values, agentId })}
            >
              <SelectTrigger id="admit-agent">
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
              workload inherits its policy in full.
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
                        may wrap: <code> will not on its own, and the dialog
                        grows to fit it instead — off the side of the viewport
                        for a realistic issuer URL. */}
                    <code className="break-all">{wildcardStem}</code>
                    {selectedAgentName
                      ? ` is admitted, and each will inherit ${selectedAgentName}'s policy. `
                      : " is admitted. "}
                    Check that the varying part is assigned by the issuer rather
                    than chosen by the caller — where a caller can influence it,
                    this admits anyone who can.
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
        </Stack>

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => handleOpenChange(false)}
            disabled={isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            onClick={() =>
              onSubmit({
                ...values,
                issuer: selectedIssuerUrl,
                subject: storedSubject,
                matchKind,
              })
            }
            disabled={!canSubmit || isPending}
          >
            <Button.Text>
              {isPending ? "Admitting…" : "Admit workload"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
