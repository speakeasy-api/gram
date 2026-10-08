import { SetupHelp } from "@/components/setup-steps/StepBlocks";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { OptionPicker, TagsField, type Option } from "../setup/SetupBlocks";
import type { AgentPickerBlock, InputBlock, TagsBlock } from "./definition";

/**
 * The custom flows' form controls. Each takes its copy from its block and its
 * value, id and problem from the sheet showing it; ids must be unique on the
 * page.
 */

function FieldHelp({
  markdown,
}: {
  markdown: string | undefined;
}): JSX.Element | null {
  return markdown === undefined ? null : <SetupHelp markdown={markdown} />;
}

/** An input or text area, with its problem or, while it has none, its help. */
export function FormInput({
  block,
  id,
  problemId = `${id}-error`,
  value,
  onChange,
  problem,
}: {
  block: InputBlock;
  id: string;
  /** The problem message's id; the input is described by it while shown. */
  problemId?: string;
  value: string;
  onChange: (value: string) => void;
  problem: string | null;
}): JSX.Element {
  const controlProps = {
    id,
    value,
    placeholder: block.placeholder,
    readOnly: block.readOnly,
    "aria-invalid": problem !== null,
    "aria-describedby": problem !== null ? problemId : undefined,
    onChange,
  };
  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{block.label}</Label>
      {block.multiline ? (
        <TextArea rows={2} {...controlProps} />
      ) : (
        <Input {...controlProps} />
      )}
      {problem !== null ? (
        <Text id={problemId} role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        <FieldHelp markdown={block.help} />
      )}
    </Stack>
  );
}

/** Picks the agent an access rule assigns, from the options given. */
export function FormAgentPicker({
  block,
  id,
  options,
  value,
  onChange,
}: {
  block: AgentPickerBlock;
  id: string;
  options: Option[];
  value: string;
  onChange: (agentId: string) => void;
}): JSX.Element {
  return (
    <OptionPicker
      id={id}
      label={block.label}
      placeholder={block.placeholder ?? ""}
      options={options}
      value={value}
      onChange={onChange}
      hint={<FieldHelp markdown={block.help} />}
    />
  );
}

/** Optional labels, checked against the server's limits. */
export function FormTags({
  block,
  id,
  tags,
  onChange,
}: {
  block: TagsBlock;
  id: string;
  tags: string[];
  onChange: (tags: string[]) => void;
}): JSX.Element {
  return (
    <TagsField
      id={id}
      label={block.label}
      placeholder={block.placeholder ?? ""}
      help={<FieldHelp markdown={block.help} />}
      tags={tags}
      onChange={onChange}
    />
  );
}
