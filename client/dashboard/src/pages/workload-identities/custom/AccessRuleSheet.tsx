import { Alert } from "@/components/ui/Alert";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { admissionValuesDiffer } from "../admissionEdit";
import type {
  AdmitSubjectFormValues,
  AdmitSubjectInitialValues,
  AdmitSubjectValues,
} from "../formValues";
import {
  buildAdmitValues,
  canAdmit,
  inferMatchKind,
  subjectRuleWarning,
} from "../subjectRule";
import { tagsProblem } from "../tagLimits";
import { FormAgentPicker, FormInput, FormTags } from "./FormBlocks";
import { FormSheet } from "./FormSheet";
import { useSheetForm } from "./useSheetForm";

interface AccessRuleSheetProps {
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

/** Allows a machine access under a trusted platform, or edits that access. */
export function AccessRuleSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  issuer,
  agents,
  initial,
}: AccessRuleSheetProps): JSX.Element {
  const isEditing = initial !== undefined;
  const { values, setValues, baseline } = useSheetForm(
    open,
    initial,
    formValues,
  );

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
      ? "This issuer does not permit wildcard matching, so a rule ending in “*” cannot be admitted under it. Give the subject in full, or turn wildcard admission back on for the issuer."
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

  return (
    <FormSheet
      open={open}
      onOpenChange={onOpenChange}
      flow={isEditing ? "editAccess" : "allowAccess"}
      canSubmit={canSubmit}
      isPending={isPending}
      onSubmit={() =>
        onSubmit(buildAdmitValues(values, issuer.issuer), baseline)
      }
      renderers={{
        input: (block) => {
          switch (block.field) {
            case "subject":
              return (
                <FormInput
                  block={block}
                  id="admit-subject"
                  problemId="admit-subject-warning"
                  value={values.subject}
                  problem={warning}
                  onChange={(subject) => setValues({ ...values, subject })}
                />
              );
            case "label":
              return (
                <FormInput
                  block={block}
                  id="admit-name"
                  value={values.name}
                  problem={null}
                  onChange={(name) => setValues({ ...values, name })}
                />
              );
            case "name":
            case "description":
            case "issuer":
            case "jwks_uri":
              return null;
          }
        },
        agent_picker: (block) => (
          <FormAgentPicker
            block={block}
            id="admit-agent"
            options={agentOptions}
            value={values.agentId}
            onChange={(agentId) => setValues({ ...values, agentId })}
          />
        ),
        wildcard_caution: () =>
          showCaution && (
            <WildcardCaution
              stem={wildcardStem}
              agentName={selectedAgentName}
            />
          ),
        tags: (block) => (
          <FormTags
            block={block}
            id="admit-tags"
            tags={values.tags}
            onChange={(tags) => setValues({ ...values, tags })}
          />
        ),
      }}
    />
  );
}

/** Names the subjects a wildcard rule admits, and the agent each inherits. */
function WildcardCaution({
  stem,
  agentName,
}: {
  stem: string;
  agentName: string;
}): JSX.Element {
  return (
    // alignTop because this body runs to several lines: a centred icon
    // drifts into the middle of the text and stops reading as a marker.
    <Alert variant="warning" alignTop>
      {/* Plain elements so the copy takes the Alert's warning
          color, which Text's own color class would override. */}
      <div className="text-sm break-words">
        <p className="font-medium">This rule admits more than one identity</p>
        <p>
          Any subject beginning{" "}
          {/* A subject is one unbroken token, so it has to be told it
            may wrap: <code> will not on its own, and the sheet
            would scroll sideways instead for a realistic issuer
            URL. */}
          <code className="break-all">{stem}</code>
          {agentName
            ? ` is admitted, and each will inherit ${agentName}'s policy. `
            : " is admitted. "}
          Check that the varying part is assigned by the issuer rather than
          chosen by the caller — where a caller can influence it, this admits
          anyone who can.
        </p>
      </div>
    </Alert>
  );
}
