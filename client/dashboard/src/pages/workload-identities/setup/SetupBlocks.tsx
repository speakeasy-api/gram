import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { CopyButton } from "@/components/ui/CopyButton";
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
import { Markdown } from "@/elements/components/Markdown";
import { useState } from "react";
import type {
  CatalogEntry,
  ChecklistItemBlock,
  SetupBlock,
} from "./definition";
import { isRelativePath } from "./origin";
import { tagsProblem } from "../tagLimits";
import type { SetupValues } from "./setupValues";
import { subjectRule, variableProblem, type VariableValues } from "./template";

const SETUP_PROSE =
  "text-sm [&_ul]:ml-6 [&_ul]:list-disc [&_li]:my-1 [&_strong]:font-semibold";

// Structural subset of an mdast node, enough to walk the tree.
interface MarkdownNode {
  type: string;
  children?: MarkdownNode[];
}

/**
 * Drops every image from definition text. Images belong in an image block,
 * which loads only from the dashboard's origin; one in markdown could point
 * the operator's browser at any host.
 */
function remarkDropImages(): (tree: unknown) => void {
  return (tree) => dropImages(tree as MarkdownNode);
}

function dropImages(node: MarkdownNode): void {
  if (node.children === undefined) return;
  node.children = node.children.filter(
    (child) => child.type !== "image" && child.type !== "imageReference",
  );
  node.children.forEach(dropImages);
}

const TEXT_REMARK_PLUGINS = [remarkDropImages];

/** The longest agent name the server accepts. */
const AGENT_NAME_MAX_LENGTH = 120;

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
  /** Creates an agent with this name and selects it; false if that failed. */
  onCreateAgent: (name: string) => Promise<boolean>;
  tags: string[];
  onTagsChange: (tags: string[]) => void;
  /** Why the values repeat an existing access rule, or null when they don't. */
  ruleConflict: string | null;
  setupValues: SetupValues;
  /** The checklist items ticked so far, by block key. */
  checkedItems: ReadonlySet<string>;
  onCheckedChange: (blockKey: string, checked: boolean) => void;
}

export function SetupBlockView({
  block,
  blockKey,
  context,
}: {
  block: SetupBlock;
  /** Identifies the block within the definition, for state kept per block. */
  blockKey: string;
  context: SetupBlockContext;
}): JSX.Element | null {
  switch (block.type) {
    case "text":
      // react-markdown escapes raw HTML, so definition text cannot inject
      // markup or script whatever its source.
      return (
        <Markdown
          className={SETUP_PROSE}
          extraRemarkPlugins={TEXT_REMARK_PLUGINS}
        >
          {block.markdown}
        </Markdown>
      );
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
          className="text-foreground block w-fit text-sm underline underline-offset-4"
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
      return <ComputedStatus setupValues={context.setupValues} />;
    case "computed":
      return (
        <ComputedValue
          label={block.label}
          help={block.help}
          value={context.setupValues.values?.[block.value]}
        />
      );
    case "checklist_item":
      return (
        <ChecklistItem block={block} blockKey={blockKey} context={context} />
      );
  }
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
  return (
    <Stack gap={2}>
      <p className="text-eyebrow">Access rule</p>
      <code className="border-border bg-card block border px-3 py-2 text-sm break-all">
        {rule.subject}
      </code>
      {context.ruleConflict !== null && (
        <Alert variant="error" alignTop>
          <div className="text-sm">{context.ruleConflict}</div>
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
  const [creating, setCreating] = useState(false);
  if (creating) {
    return <NewAgentForm context={context} onDone={() => setCreating(false)} />;
  }
  return (
    <Stack gap={2}>
      <OptionPicker
        id="setup-agent"
        label="Agent"
        placeholder="Select an agent"
        options={context.agents}
        value={context.agentId}
        onChange={context.onAgentChange}
        hint={context.agentsUnavailable}
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
  context,
  onDone,
}: {
  context: SetupBlockContext;
  onDone: () => void;
}): JSX.Element {
  const [name, setName] = useState(context.entry.displayName);
  const [saving, setSaving] = useState(false);
  const trimmed = name.trim();

  const create = async () => {
    if (trimmed === "" || saving) return;
    setSaving(true);
    const created = await context.onCreateAgent(trimmed);
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

/**
 * Which user session issuer the computed values belong to, or why there are
 * none to show.
 */
function ComputedStatus({
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
      <CopyableValue label={label} value={value} />
      {help !== undefined && (
        <Text muted small>
          {help}
        </Text>
      )}
    </Stack>
  );
}

/**
 * One console field or control and what to do with it, with a checkbox the
 * operator ticks as they switch between the console and this sheet.
 */
function ChecklistItem({
  block,
  blockKey,
  context,
}: {
  block: ChecklistItemBlock;
  blockKey: string;
  context: SetupBlockContext;
}): JSX.Element {
  const id = `setup-check-${blockKey}`;
  const checked = context.checkedItems.has(blockKey);
  return (
    <div className="border-border bg-card flex items-start gap-3 border p-3">
      <Checkbox
        id={id}
        className="mt-0.5"
        checked={checked}
        onCheckedChange={(next) =>
          context.onCheckedChange(blockKey, next === true)
        }
      />
      <Stack gap={1} className="min-w-0 flex-1">
        <Label
          htmlFor={id}
          className={checked ? "text-muted-foreground line-through" : undefined}
        >
          {block.label}
        </Label>
        {block.value !== undefined ? (
          <CopyableValue
            label={block.label}
            value={context.setupValues.values?.[block.value]}
          />
        ) : (
          <Markdown
            className="text-muted-foreground text-sm [&_strong]:font-semibold"
            extraRemarkPlugins={TEXT_REMARK_PLUGINS}
          >
            {block.instruction}
          </Markdown>
        )}
        {block.help !== undefined && (
          <Text muted small>
            {block.help}
          </Text>
        )}
      </Stack>
    </div>
  );
}

/** A value Speakeasy derives, with a button that copies it. */
function CopyableValue({
  label,
  value,
}: {
  label: string;
  value: string | undefined;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex items-center gap-2 border px-3 py-1.5">
      <code className="min-w-0 flex-1 text-sm break-all">{value ?? "—"}</code>
      {value !== undefined && value !== "" && (
        <CopyButton text={value} size="sm" tooltip={`Copy ${label}`} />
      )}
    </div>
  );
}
