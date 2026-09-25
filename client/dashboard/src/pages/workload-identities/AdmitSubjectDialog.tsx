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
  composeSubject,
  type MatchKind,
  shouldSwitchToWildcard,
  subjectRuleWarning,
  typedSubjectForMatchKind,
} from "./subjectRule";

export interface AdmitSubjectValues {
  issuer: string;
  subject: string;
  matchKind: MatchKind;
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

const EMPTY: AdmitSubjectValues = {
  issuer: "",
  subject: "",
  matchKind: "exact",
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
  const [values, setValues] = useState<AdmitSubjectValues>(EMPTY);

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
  const storedSubject = composeSubject(values.matchKind, values.subject);
  const warning = subjectRuleWarning(values.matchKind, storedSubject);

  // Stated where the rule is written rather than asked when the issuer is
  // registered. An operator registering an issuer has no rule in mind yet, and
  // whether a wildcard is sound is a judgement about their own platform. Naming
  // the stem and the agent makes the reach of the rule concrete.
  const wildcardStem =
    values.matchKind === "wildcard" && storedSubject.length > 1
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
    matchKindPermitted: values.matchKind !== "wildcard" || wildcardAvailable,
  });

  const handleIssuerChange = (issuer: string) => {
    const next = issuers.find((candidate) => candidate.issuer === issuer);
    setValues({
      ...values,
      issuer,
      // Switching to an issuer that forbids wildcards must not leave a wildcard
      // selected, which would submit a rule the server refuses.
      matchKind: next?.allowWildcardAdmission ? values.matchKind : "exact",
    });
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
            <Label htmlFor="admit-match-kind">Match</Label>
            <Select
              value={values.matchKind}
              onValueChange={(matchKind) =>
                setValues({
                  ...values,
                  matchKind: matchKind as MatchKind,
                  subject: typedSubjectForMatchKind(
                    values.subject,
                    matchKind as MatchKind,
                  ),
                })
              }
            >
              <SelectTrigger id="admit-match-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="exact">Exact subject</SelectItem>
                <SelectItem value="wildcard" disabled={!wildcardAvailable}>
                  Wildcard
                </SelectItem>
              </SelectContent>
            </Select>
            {!wildcardAvailable && selectedIssuerUrl.length > 0 && (
              <Text muted small>
                This issuer does not permit wildcard matching. Turn on wildcard
                admission on the issuer to use it.
              </Text>
            )}
          </Stack>

          <Stack gap={2}>
            <Label htmlFor="admit-subject">Subject</Label>
            <Stack direction="horizontal" align="center" gap={2}>
              <Input
                id="admit-subject"
                className="flex-1"
                value={values.subject}
                placeholder={
                  values.matchKind === "wildcard"
                    ? "wimse://identity.example.com/org/acme/agent/"
                    : "wimse://identity.example.com/org/acme/agent/a-1"
                }
                aria-describedby={warning ? "admit-subject-warning" : undefined}
                aria-invalid={warning !== null}
                onChange={(value) =>
                  shouldSwitchToWildcard(
                    value,
                    values.matchKind,
                    wildcardAvailable,
                  )
                    ? setValues({
                        ...values,
                        matchKind: "wildcard",
                        subject: typedSubjectForMatchKind(value, "wildcard"),
                      })
                    : setValues({ ...values, subject: value })
                }
              />
              {/* The terminator, shown rather than typed. It is part of the
                  stored subject, so an operator can see the breadth of the rule
                  they are about to create without having to keep it in step with
                  the match kind themselves. */}
              {values.matchKind === "wildcard" && (
                <Text aria-hidden className="font-mono text-base">
                  *
                </Text>
              )}
            </Stack>
            {warning !== null ? (
              <Text id="admit-subject-warning" role="alert" small destructive>
                {warning}
              </Text>
            ) : (
              <Text muted small>
                {values.matchKind === "wildcard"
                  ? `Stored as ${storedSubject || "the value above plus *"}, and compared against anything beginning with it. Gram does not normalize it.`
                  : "Stored and compared exactly as entered. Gram does not normalize it."}
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
              <Text role="status" small className="text-amber-700">
                This admits <strong>any</strong> subject beginning{" "}
                <code>{wildcardStem}</code>
                {selectedAgentName
                  ? `, and each will inherit ${selectedAgentName}'s policy. `
                  : ". "}
                Check that the varying part is assigned by the issuer rather
                than chosen by the caller — where a caller can influence it,
                this admits anyone who can.
              </Text>
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
