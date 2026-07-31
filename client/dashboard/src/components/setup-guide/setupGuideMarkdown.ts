// Setup guides are authored as standalone markdown files, so they carry two
// constructs the dashboard's renderer (react-markdown + remark-gfm) does not
// understand: a YAML frontmatter block, and heading anchors written as a
// trailing `{#custom-id}`. Left alone, the first renders as a horizontal rule
// followed by a heading reading "setup_version: 1", and the second prints the
// braces verbatim while the links pointing at them go nowhere.

// Anchored at index 0 so a `---` thematic break further down the guide survives.
const LEADING_FRONTMATTER = /^---\r?\n[\s\S]*?\r?\n---[^\S\r\n]*(?:\r?\n|$)/;

// Guides open with an H1 repeating their own title, which the panel header
// already shows.
const LEADING_H1 = /^#[^\S\r\n]+[^\r\n]*(?:\r?\n|$)/;

const TRAILING_HEADING_ID = /[^\S\r\n]*\{#([A-Za-z0-9_-]+)\}[^\S\r\n]*$/;

const HTML_COMMENT = /^<!--[\s\S]*-->$/;

/**
 * Strips a published setup guide's frontmatter block and its duplicate title
 * heading, leaving the body ready to render.
 */
export function normalizeSetupGuideMarkdown(markdown: string): string {
  return markdown
    .replace(LEADING_FRONTMATTER, "")
    .trimStart()
    .replace(LEADING_H1, "")
    .trimStart();
}

// Structural subset of an mdast node. Declared here rather than imported
// because @types/mdast is not a dependency of the dashboard.
interface MarkdownNode {
  type: string;
  value?: string;
  children?: MarkdownNode[];
  data?: { hProperties?: Record<string, unknown> };
}

/**
 * Namespaces a heading id by the guide that authored it.
 *
 * Matched guides are stacked into one document, and their headings collide by
 * construction: the Gram half of every guide is templated from one canonical
 * section, so ids like `#connect-speakeasy-credentials` appear in all of them.
 * Left as authored, an anchor in the second guide scrolls to the first guide's
 * copy of that heading.
 */
export function scopedHeadingId(guideSlug: string, id: string): string {
  return `${guideSlug}--${id}`;
}

/**
 * remark plugin covering the two things a published guide expects of its
 * renderer that GFM does not provide:
 *
 *   - `## Enable Box AI {#enable-box-ai}` becomes a heading carrying an id, so
 *     the in-document links guides use to cross-reference their own sections
 *     resolve instead of dangling. The id is scoped to `guideSlug`; the
 *     matching rewrite for links lives with the panel that renders them.
 *   - HTML comments are dropped. Guides carry authoring notes in them (screenshot
 *     placeholders, mostly), which react-markdown otherwise prints as body text.
 *
 * Operates on nodes rather than the raw source, so neither a `{#...}` nor a
 * `<!-- -->` inside a fenced code block is touched.
 */
export function remarkSetupGuide({
  guideSlug,
}: {
  guideSlug: string;
}): (tree: unknown) => void {
  return (tree) => {
    visitSetupGuideNode(tree as MarkdownNode, guideSlug);
  };
}

function visitSetupGuideNode(node: MarkdownNode, guideSlug: string): void {
  if (node.type === "heading") {
    liftHeadingId(node, guideSlug);
  }

  if (!node.children) return;

  node.children = node.children.filter((child) => !isHtmlComment(child));
  for (const child of node.children) {
    visitSetupGuideNode(child, guideSlug);
  }
}

function isHtmlComment(node: MarkdownNode): boolean {
  return node.type === "html" && HTML_COMMENT.test(node.value?.trim() ?? "");
}

function liftHeadingId(heading: MarkdownNode, guideSlug: string): void {
  const children = heading.children ?? [];
  const last = children[children.length - 1];
  if (last?.type !== "text" || last.value === undefined) return;

  const match = TRAILING_HEADING_ID.exec(last.value);
  const id = match?.[1];
  if (!match || !id) return;

  last.value = last.value.slice(0, match.index).trimEnd();
  heading.data = {
    ...heading.data,
    hProperties: {
      ...heading.data?.hProperties,
      id: scopedHeadingId(guideSlug, id),
    },
  };
}
