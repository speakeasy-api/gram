import { Checkbox } from "@/components/ui/Checkbox";
import { CopyButton } from "@/components/ui/CopyButton";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { Markdown } from "@/elements/components/Markdown";
import { isRelativePath } from "./origin";

const SETUP_PROSE =
  "text-sm [&_ul]:ml-6 [&_ul]:list-disc [&_li]:my-1 [&_strong]:font-semibold";

const SETUP_HELP =
  "text-muted-foreground text-sm [&_p]:leading-5 [&_code]:border-0 [&_code]:bg-transparent [&_code]:p-0 [&_code]:font-normal";

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

/**
 * Definition markdown, without its images. react-markdown escapes raw HTML,
 * so the text cannot inject markup or script whatever its source.
 */
function SafeMarkdown({
  markdown,
  className,
}: {
  markdown: string;
  className: string;
}): JSX.Element {
  return (
    <Markdown className={className} extraRemarkPlugins={TEXT_REMARK_PLUGINS}>
      {markdown}
    </Markdown>
  );
}

/** A passage of setup prose. */
export function SetupText({ markdown }: { markdown: string }): JSX.Element {
  return <SafeMarkdown markdown={markdown} className={SETUP_PROSE} />;
}

/**
 * Markdown help under a form control, in the muted small type of a hint.
 * Inline code keeps the surrounding type rather than the boxed chat style.
 */
export function SetupHelp({ markdown }: { markdown: string }): JSX.Element {
  return <SafeMarkdown markdown={markdown} className={SETUP_HELP} />;
}

/** An illustration, shown only when src is a path on the dashboard's origin. */
export function SetupImage({
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

/** A link out, opened in a new tab without access to this window. */
export function SetupLink({
  href,
  label,
}: {
  href: string;
  label: string;
}): JSX.Element {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-foreground block w-fit text-sm underline underline-offset-4"
    >
      {label}
    </a>
  );
}

/** A value under its own heading, with a button that copies it. */
export function LabeledValue({
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
 * What a checklist item asks for: a value to copy (absent while it is not yet
 * known), or markdown saying what to do.
 */
export type ChecklistItemDetail =
  | { kind: "copy"; value: string | undefined }
  | { kind: "markdown"; markdown: string };

/**
 * One field or control to deal with elsewhere, with a checkbox the operator
 * ticks as they switch between there and this setup.
 */
export function ChecklistItem({
  id,
  label,
  detail,
  help,
  checked,
  onCheckedChange,
}: {
  /** The checkbox's id, unique on the page. */
  id: string;
  label: string;
  detail: ChecklistItemDetail;
  help?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex items-start gap-3 border p-3">
      <Checkbox
        id={id}
        className="mt-0.5"
        checked={checked}
        onCheckedChange={(next) => onCheckedChange(next === true)}
      />
      <Stack gap={1} className="min-w-0 flex-1">
        <Label
          htmlFor={id}
          className={checked ? "text-muted-foreground line-through" : undefined}
        >
          {label}
        </Label>
        {detail.kind === "copy" ? (
          <CopyableValue label={label} value={detail.value} />
        ) : (
          <SafeMarkdown
            markdown={detail.markdown}
            className="text-muted-foreground text-sm [&_strong]:font-semibold"
          />
        )}
        {help !== undefined && (
          <Text muted small>
            {help}
          </Text>
        )}
      </Stack>
    </div>
  );
}

/** A value in a code box, with a button that copies it once there is one. */
export function CopyableValue({
  label,
  value,
}: {
  /** Names the value in the copy button's tooltip. */
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
