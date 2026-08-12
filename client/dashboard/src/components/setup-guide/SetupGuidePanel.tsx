import { docsUrl, soleGuide } from "@/components/setup-guide/guideDocs";
import {
  normalizeSetupGuideMarkdown,
  remarkSetupGuide,
  scopedHeadingId,
} from "@/components/setup-guide/setupGuideMarkdown";
import { Text } from "@/components/ui/Text";
import { Markdown, type MarkdownProps } from "@/elements/components/Markdown";
import type { LinkResolver } from "@/elements/types";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";
import { Badge } from "@/components/ui/Badge";
import { ExternalLink } from "lucide-react";
import { useMemo } from "react";

// Guide headings are sized for a full documentation page; scale them to a
// panel that is a few hundred pixels wide.
//
// Lists get a wider gutter than `Markdown`'s 1.5rem default, and ordered lists
// a wider one still. Markers sit outside the content box and are right-aligned
// against it, so a marker's own width eats into the indent: at 1.5rem a
// two-digit marker lands flush with the surrounding paragraphs, and guides
// routinely run past ten steps. 3rem puts the number column either side of
// where a bullet falls at 2rem, so both list kinds read as equally indented.
const GUIDE_PROSE =
  "text-sm [&_h2]:text-lg [&_h3]:text-base [&_h4]:text-sm [&_ul]:ml-8 [&_ol]:ml-12";

// A guide's two halves are authored as sibling files, so they cross-reference
// each other by filename: `external.md#create-oauth-client`. Both halves render
// as sections of this one panel, where that path resolves to nothing.
const GUIDE_CROSS_REFERENCE = /^(?:\.\/)?[\w.-]+\.md(?:#([\w-]+))?$/;

// An anchor a half uses within itself: `#create-oauth-client`.
const GUIDE_ANCHOR = /^#([\w-]+)$/;

/**
 * Decides where each link in one guide points.
 *
 * Provider consoles and docs open in a new tab, so following one does not
 * abandon a half-finished setup in the panel behind it. Every link naming a
 * heading, whether a cross-reference to the guide's other half or an anchor
 * within the current one, lands on that heading's scoped id, so it stays
 * inside the guide it was written for even when a second guide on the page
 * repeats the heading. A cross-reference naming no heading is dropped to plain
 * text rather than left as a link to nowhere.
 *
 * Only http(s) is claimed as external: `Markdown` skips its URL sanitizer for
 * any href a resolver takes responsibility for, and guides are third-party
 * content.
 */
function guideLinkResolver(guideSlug: string): LinkResolver {
  const inThisGuide = (id: string) => ({
    href: `#${scopedHeadingId(guideSlug, id)}`,
  });

  return (href) => {
    const anchor = GUIDE_ANCHOR.exec(href)?.[1];
    if (anchor) return inThisGuide(anchor);

    const crossReference = GUIDE_CROSS_REFERENCE.exec(href);
    if (crossReference) {
      const id = crossReference[1];
      return id ? inThisGuide(id) : { href: null };
    }

    let url: URL;
    try {
      url = new URL(href, window.location.href);
    } catch {
      return null;
    }

    const isWebLink = url.protocol === "http:" || url.protocol === "https:";
    if (!isWebLink || url.origin === window.location.origin) return null;

    return { href, target: "_blank" };
  };
}

/**
 * The setup guide as side-panel content.
 *
 * Refetches from the same lookup keys the callout used rather than being
 * handed the guides, because the panel outlives the page that opened it. The
 * query is already cached by then, so this costs a cache read.
 */
export function SetupGuidePanel({
  registrySpecifier,
  serverUrl,
}: {
  registrySpecifier?: string;
  serverUrl?: string;
}): React.JSX.Element | null {
  const { data } = useGetMCPSetupDocs(
    { registrySpecifier, serverUrl },
    undefined,
    {
      enabled: !!registrySpecifier || !!serverUrl,
      throwOnError: false,
    },
  );

  const guides = data?.guides ?? [];
  if (guides.length === 0) return null;

  const only = soleGuide(guides);

  return (
    <div className="flex flex-col gap-6 px-6 pt-5 pb-8">
      {only?.summary && (
        <Text variant="small" className="text-muted-foreground">
          {only.summary}
        </Text>
      )}
      {/* Two guides are stacked in the order the endpoint ranked them. */}
      <div className="flex flex-col gap-10">
        {guides.map((guide) => (
          <div key={guide.slug} className="flex flex-col gap-6">
            {/* One guide is linked from the panel header. Two have no single
                page that could go up there, so each carries its own. */}
            {!only && (
              <Badge asChild variant="information" className="self-start">
                <a
                  href={docsUrl(guide)}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Badge.Text>Open documentation</Badge.Text>
                  <Badge.RightIcon>
                    <ExternalLink />
                  </Badge.RightIcon>
                </a>
              </Badge>
            )}
            <SetupGuideSection
              heading={`Set up in ${guide.title}`}
              markdown={guide.externalMarkdown}
              guideSlug={guide.slug}
            />
            <SetupGuideSection
              heading="Set up in Gram"
              markdown={guide.speakeasyMarkdown}
              guideSlug={guide.slug}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

function SetupGuideSection({
  heading,
  markdown,
  guideSlug,
}: {
  heading: string;
  markdown: string;
  /** Which guide's headings this half links to and answers for. */
  guideSlug: string;
}): React.JSX.Element | null {
  // Memoized because <Markdown /> is: a fresh plugin list or resolver on every
  // render would defeat it.
  const remarkPlugins = useMemo<MarkdownProps["extraRemarkPlugins"]>(
    () => [[remarkSetupGuide, { guideSlug }]],
    [guideSlug],
  );
  const resolveLink = useMemo(() => guideLinkResolver(guideSlug), [guideSlug]);

  const body = normalizeSetupGuideMarkdown(markdown);
  if (!body) return null;

  return (
    <section className="flex flex-col gap-3">
      <Text className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
        {heading}
      </Text>
      <Markdown
        className={GUIDE_PROSE}
        extraRemarkPlugins={remarkPlugins}
        resolveLink={resolveLink}
      >
        {body}
      </Markdown>
    </section>
  );
}
