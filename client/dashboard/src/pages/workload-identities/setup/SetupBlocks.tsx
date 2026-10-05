import { Alert } from "@/components/ui/Alert";
import { CopyButton } from "@/components/ui/CopyButton";
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
import { TagInput } from "@/components/ui/TagInput";
import { Text } from "@/components/ui/Text";
import { Markdown } from "@/elements/components/Markdown";
import type { CatalogEntry, SetupBlock } from "./definition";
import { tagsProblem } from "../tagLimits";
import type { SetupValues } from "./setupValues";
import { subjectRule, variableProblem, type VariableValues } from "./template";

const SETUP_PROSE =
  "text-sm [&_ul]:ml-6 [&_ul]:list-disc [&_li]:my-1 [&_strong]:font-semibold";

export interface Option {
  id: string;
  name: string;
}

/** Everything a block may read or change. */
export interface SetupBlockContext {
  entry: CatalogEntry;
  values: VariableValues;
  onValueChange: (key: string, value: string) => void;
  agents: Option[];
  agentsUnavailable: string | null;
  agentId: string;
  onAgentChange: (agentId: string) => void;
  tags: string[];
  onTagsChange: (tags: string[]) => void;
  setupValues: SetupValues;
}

export function SetupBlockView({
  block,
  context,
}: {
  block: SetupBlock;
  context: SetupBlockContext;
}): JSX.Element | null {
  switch (block.type) {
    case "text":
      // react-markdown escapes raw HTML, so definition text cannot inject
      // markup or script whatever its source.
      return <Markdown className={SETUP_PROSE}>{block.markdown}</Markdown>;
    case "image":
      return (
        <SetupImage src={block.src} alt={block.alt} caption={block.caption} />
      );
    case "link":
      return (
        <a
          href={block.href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-foreground text-sm underline underline-offset-4"
        >
          {block.label}
        </a>
      );
    case "field":
      return <VariableField variableKey={block.variable} context={context} />;
    case "subject_rule":
      return <SubjectRulePreview context={context} />;
    case "agent_picker":
      return <AgentPicker context={context} />;
    case "tags":
      return <TagsField context={context} />;
    case "computed_status":
      return <ComputedStatus reason={context.setupValues.unavailableReason} />;
    case "computed":
      return (
        <ComputedValue
          label={block.label}
          help={block.help}
          value={context.setupValues.values?.[block.value]}
        />
      );
  }
}

/**
 * Only same-origin paths are rendered. Images ship with the dashboard for now,
 * and a definition must not be able to point the operator's browser at an
 * arbitrary host.
 */
export function isRelativePath(src: string): boolean {
  return src.startsWith("/") && !src.startsWith("//");
}

function SetupImage({
  src,
  alt,
  caption,
}: {
  src: string;
  alt: string;
  caption?: string;
}): JSX.Element | null {
  if (!isRelativePath(src)) {
    return null;
  }
  return (
    <figure className="border-border overflow-hidden border">
      <img src={src} alt={alt} className="w-full" />
      {caption !== undefined && (
        <figcaption className="border-border bg-secondary/40 text-muted-foreground border-t px-3 py-2 text-xs leading-relaxed">
          {caption}
        </figcaption>
      )}
    </figure>
  );
}

function VariableField({
  variableKey,
  context,
}: {
  variableKey: string;
  context: SetupBlockContext;
}): JSX.Element | null {
  const variable = context.entry.variables.find((v) => v.key === variableKey);
  if (variable === undefined) {
    return null;
  }
  const value = context.values[variable.key] ?? "";
  // Say nothing about an untouched field; an empty value already keeps Next
  // disabled.
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
        onChange={(next) => context.onValueChange(variable.key, next)}
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

function SubjectRulePreview({
  context,
}: {
  context: SetupBlockContext;
}): JSX.Element | null {
  const rule = subjectRule(context.entry, context.values);
  if (rule === null) {
    return null;
  }
  const agentName = context.agents.find((a) => a.id === context.agentId)?.name;

  return (
    <Stack gap={2}>
      <p className="text-eyebrow">Access rule</p>
      <code className="border-border bg-card block border px-3 py-2 text-sm break-all">
        {rule.subject}
      </code>
      {rule.matchKind === "wildcard" && (
        <Alert variant="warning" alignTop>
          <div className="text-sm break-words">
            <p className="font-medium">
              This rule admits more than one identity
            </p>
            <p>
              Every subject beginning{" "}
              <code className="break-all">{rule.subject.slice(0, -1)}</code> is
              allowed
              {agentName ? `, and each acts under ${agentName}'s policy` : ""}.
              That includes channels created later.
            </p>
          </div>
        </Alert>
      )}
    </Stack>
  );
}

function OptionPicker({
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
  hint: string | null;
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
      {hint !== null && (
        <Text muted small>
          {hint}
        </Text>
      )}
    </Stack>
  );
}

function AgentPicker({ context }: { context: SetupBlockContext }): JSX.Element {
  return (
    <OptionPicker
      id="setup-agent"
      label="Agent"
      placeholder="Select an agent"
      options={context.agents}
      value={context.agentId}
      onChange={context.onAgentChange}
      hint={
        context.agentsUnavailable ??
        "Create one under Agents if none fits, then come back."
      }
    />
  );
}

function TagsField({ context }: { context: SetupBlockContext }): JSX.Element {
  const problem = tagsProblem(context.tags);
  return (
    <Stack gap={2}>
      <Label htmlFor="setup-tags">Tags (optional)</Label>
      <TagInput
        id="setup-tags"
        value={context.tags}
        placeholder="support, production"
        error={problem !== null}
        ariaDescribedBy={problem !== null ? "setup-tags-error" : undefined}
        onChange={context.onTagsChange}
      />
      {problem !== null ? (
        <Text id="setup-tags-error" role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        <Text muted small>
          Labels for finding this access later. Not used for matching.
        </Text>
      )}
    </Stack>
  );
}

function ComputedStatus({
  reason,
}: {
  reason: string | null;
}): JSX.Element | null {
  if (reason === null) {
    return null;
  }
  return (
    <Alert variant="info" alignTop>
      <div className="text-sm">{reason}</div>
    </Alert>
  );
}

function ComputedValue({
  label,
  help,
  value,
}: {
  label: string;
  help?: string;
  value: string | undefined;
}): JSX.Element {
  return (
    <Stack gap={1}>
      <p className="text-eyebrow">{label}</p>
      <div className="border-border bg-card flex items-center gap-2 border px-3 py-1.5">
        <code className="min-w-0 flex-1 text-sm break-all">{value ?? "—"}</code>
        {value !== undefined && value !== "" && (
          <CopyButton text={value} size="sm" tooltip={`Copy ${label}`} />
        )}
      </div>
      {help !== undefined && (
        <Text muted small>
          {help}
        </Text>
      )}
    </Stack>
  );
}
