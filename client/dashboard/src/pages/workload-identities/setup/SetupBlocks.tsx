import {
  ChecklistItem,
  type ChecklistItemDetail,
} from "@/components/setup-steps/StepBlocks";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { TagInput } from "@/components/ui/TagInput";
import { Text } from "@/components/ui/Text";
import { type ReactNode, useState } from "react";
import type { CatalogVariable, ChecklistItemBlock } from "./definition";
import { tagsProblem } from "../tagLimits";
import type { SetupValues } from "./setupValues";
import { variableProblem } from "./template";

/**
 * The blocks of a catalog platform's guided setup that read or change its
 * state. The generic ones, such as text and images, are shared with any
 * step-by-step setup.
 */

/** The longest agent name the server accepts. */
const AGENT_NAME_MAX_LENGTH = 120;

export interface Option {
  id: string;
  name: string;
}

/** An input for one variable, with why its value cannot be used. */
export function VariableField({
  variable,
  value,
  onChange,
}: {
  variable: CatalogVariable;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  // Say nothing about an untouched field; an empty value already keeps
  // Continue disabled.
  const problem =
    value.trim().length > 0 ? variableProblem(variable, value) : null;
  const id = `setup-${variable.key}`;

  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{variable.label}</Label>
      <Input
        id={id}
        value={value}
        placeholder={variable.placeholder}
        aria-invalid={problem !== null}
        aria-describedby={problem !== null ? `${id}-error` : undefined}
        onChange={onChange}
      />
      {problem !== null ? (
        <Text id={`${id}-error`} role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        variable.help !== undefined && (
          <Text muted small>
            {variable.help}
          </Text>
        )
      )}
    </Stack>
  );
}

/** The subject the access rule will match, and why it is taken if it is. */
export function SubjectRulePreview({
  subject,
  conflict,
}: {
  subject: string;
  /** Why the rule repeats an existing access rule, or null when it doesn't. */
  conflict: string | null;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <p className="text-eyebrow">Access rule</p>
      <code className="border-border bg-card block border px-3 py-2 text-sm break-all">
        {subject}
      </code>
      {conflict !== null && (
        <Alert variant="error" alignTop>
          <div className="text-sm">{conflict}</div>
        </Alert>
      )}
    </Stack>
  );
}

/** A labeled select over options, with an optional hint beneath it. */
export function OptionPicker({
  id,
  label,
  placeholder,
  options,
  value,
  onChange,
  hint,
}: {
  id: string;
  label: string;
  placeholder: string;
  options: Option[];
  value: string;
  onChange: (value: string) => void;
  /** Shown under the select. */
  hint: ReactNode;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.id} value={option.id}>
              {option.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {hint}
    </Stack>
  );
}

/** Picks the agent an access rule assigns, or creates one in place. */
export function AgentPicker({
  agents,
  unavailableReason,
  agentId,
  onAgentChange,
  newAgentName,
  onCreateAgent,
}: {
  agents: Option[];
  /** Why there is no agent to pick, or null. */
  unavailableReason: string | null;
  agentId: string;
  onAgentChange: (agentId: string) => void;
  /** The name a new agent starts with. */
  newAgentName: string;
  /** Creates an agent with this name and selects it; false if that failed. */
  onCreateAgent: (name: string) => Promise<boolean>;
}): JSX.Element {
  const [creating, setCreating] = useState(false);
  if (creating) {
    return (
      <NewAgentForm
        initialName={newAgentName}
        onCreateAgent={onCreateAgent}
        onDone={() => setCreating(false)}
      />
    );
  }
  return (
    <Stack gap={2}>
      <OptionPicker
        id="setup-agent"
        label="Agent"
        placeholder="Select an agent"
        options={agents}
        value={agentId}
        onChange={onAgentChange}
        hint={
          unavailableReason !== null && (
            <Text muted small>
              {unavailableReason}
            </Text>
          )
        }
      />
      <div>
        <Button variant="tertiary" size="sm" onClick={() => setCreating(true)}>
          Create a new agent
        </Button>
      </div>
    </Stack>
  );
}

function NewAgentForm({
  initialName,
  onCreateAgent,
  onDone,
}: {
  initialName: string;
  onCreateAgent: (name: string) => Promise<boolean>;
  onDone: () => void;
}): JSX.Element {
  // By code point, as the server counts the limit.
  const [name, setName] = useState(() =>
    Array.from(initialName).slice(0, AGENT_NAME_MAX_LENGTH).join(""),
  );
  const [saving, setSaving] = useState(false);
  const trimmed = name.trim();

  const create = async () => {
    if (trimmed === "" || saving) return;
    setSaving(true);
    const created = await onCreateAgent(trimmed);
    setSaving(false);
    if (created) onDone();
  };

  return (
    <Stack gap={2}>
      <Label htmlFor="setup-new-agent">New agent name</Label>
      <Input
        id="setup-new-agent"
        value={name}
        maxLength={AGENT_NAME_MAX_LENGTH}
        onChange={setName}
      />
      <Text muted small>
        A new agent can reach nothing until you give it a policy under Agents.
      </Text>
      <Stack direction="horizontal" gap={2}>
        <Button
          size="sm"
          disabled={trimmed === "" || saving}
          onClick={() => void create()}
        >
          {saving ? "Creating…" : "Create agent"}
        </Button>
        <Button variant="tertiary" size="sm" onClick={onDone}>
          Cancel
        </Button>
      </Stack>
    </Stack>
  );
}

/**
 * Optional labels for the access rule. The id must be unique on the page; it
 * also names the error message.
 */
export function TagsField({
  tags,
  onChange,
  id = "setup-tags",
  label = "Tags (optional)",
  placeholder = "support, production",
  help = (
    <Text muted small>
      Labels for finding this access later. Not used for matching.
    </Text>
  ),
}: {
  tags: string[];
  onChange: (tags: string[]) => void;
  id?: string;
  label?: string;
  placeholder?: string;
  /** Shown while the tags have no problem. */
  help?: ReactNode;
}): JSX.Element {
  const problem = tagsProblem(tags);
  const errorId = `${id}-error`;
  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{label}</Label>
      <TagInput
        id={id}
        value={tags}
        placeholder={placeholder}
        error={problem !== null}
        ariaDescribedBy={problem !== null ? errorId : undefined}
        onChange={onChange}
      />
      {problem !== null ? (
        <Text id={errorId} role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        help
      )}
    </Stack>
  );
}

/**
 * Which user session issuer the computed values belong to, or why there are
 * none to show.
 */
export function ComputedStatus({
  setupValues,
}: {
  setupValues: SetupValues;
}): JSX.Element {
  if (setupValues.unavailableReason !== null) {
    return (
      <Alert variant="info" alignTop>
        <div className="text-sm">{setupValues.unavailableReason}</div>
      </Alert>
    );
  }
  return (
    <Stack gap={2}>
      <Label htmlFor="setup-token-endpoint">User session issuer</Label>
      <Select
        value={setupValues.selectedEndpointId}
        onValueChange={setupValues.onEndpointChange}
      >
        <SelectTrigger id="setup-token-endpoint" className="w-full">
          <SelectValue placeholder="Select an issuer" />
        </SelectTrigger>
        <SelectContent>
          {setupValues.endpointGroups.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.options.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
      <Text muted small>
        Tokens are issued for the organization or project that owns this issuer.
      </Text>
    </Stack>
  );
}

/**
 * A checklist item, with the value it names from the server's derivation.
 * Changes report the item's block key, so ticks can be kept per block.
 */
export function CatalogChecklistItem({
  block,
  blockKey,
  computedValues,
  checked,
  onCheckedChange,
}: {
  block: ChecklistItemBlock;
  blockKey: string;
  computedValues: SetupValues["values"];
  checked: boolean;
  onCheckedChange: (blockKey: string, checked: boolean) => void;
}): JSX.Element {
  const detail: ChecklistItemDetail =
    block.value !== undefined
      ? { kind: "copy", value: computedValues?.[block.value] }
      : { kind: "markdown", markdown: block.instruction };
  return (
    <ChecklistItem
      id={`setup-check-${blockKey}`}
      label={block.label}
      detail={detail}
      help={block.help}
      checked={checked}
      onCheckedChange={(next) => onCheckedChange(blockKey, next)}
    />
  );
}
