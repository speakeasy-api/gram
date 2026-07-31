import { docsUrl, soleGuide } from "@/components/setup-guide/guideDocs";
import {
  normalizeSetupGuideMarkdown,
  remarkSetupGuide,
} from "@/components/setup-guide/setupGuideMarkdown";
import { Type } from "@/components/ui/type";
import { Markdown } from "@/elements/components/Markdown";
import type { ResolvedLink } from "@/elements/types";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";
import { Badge } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";

// Module-level so the memoized <Markdown /> isn't handed a new array each render.
const SETUP_GUIDE_REMARK_PLUGINS = [remarkSetupGuide];

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
const GUIDE_CROSS_REFERENCE = /^(?:\.\/)?[\w.-]+\.md(#[\w-]+)?$/;

/**
 * Decides where each link in a guide points.
 *
 * Provider consoles and docs open in a new tab, so following one does not
 * abandon a half-finished setup in the panel behind it. Cross-references
 * between the two halves collapse to the heading the remark plugin tagged,
 * which is in this same panel; one that names no heading is dropped to plain
 * text rather than left as a link to nowhere. Anything else, including the
 * `#section` anchors a half uses within itself, is left alone.
 *
 * Only http(s) is claimed as external: `Markdown` skips its URL sanitizer for
 * any href a resolver takes responsibility for, and guides are third-party
 * content.
 */
function resolveGuideLink(href: string): ResolvedLink | null {
  const crossReference = GUIDE_CROSS_REFERENCE.exec(href);
  if (crossReference) return { href: crossReference[1] ?? null };

  let url: URL;
  try {
    url = new URL(href, window.location.href);
  } catch {
    return null;
  }

  const isWebLink = url.protocol === "http:" || url.protocol === "https:";
  if (!isWebLink || url.origin === window.location.origin) return null;

  return { href, target: "_blank" };
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
        <Type variant="small" className="text-muted-foreground">
          {only.summary}
        </Type>
      )}
      {/* Two guides are stacked in the order the endpoint ranked them. */}
      <div className="flex flex-col gap-10">
        {guides.map((guide) => (
          <div key={guide.slug} className="flex flex-col gap-6">
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
            <SetupGuideSection
              heading={`Set up in ${guide.title}`}
              markdown={guide.externalMarkdown}
            />
            <SetupGuideSection
              heading="Set up in Gram"
              markdown={guide.speakeasyMarkdown}
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
}: {
  heading: string;
  markdown: string;
}): React.JSX.Element | null {
  const body = normalizeSetupGuideMarkdown(markdown);
  if (!body) return null;

  return (
    <section className="flex flex-col gap-3">
      <Type className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
        {heading}
      </Type>
      <Markdown
        className={GUIDE_PROSE}
        extraRemarkPlugins={SETUP_GUIDE_REMARK_PLUGINS}
        resolveLink={resolveGuideLink}
      >
        {body}
      </Markdown>
    </section>
  );
}
