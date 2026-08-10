import {
  docsUrl,
  setupGuideHeading,
  soleGuide,
} from "@/components/setup-guide/guideDocs";
import { useSidePanel } from "@/components/side-panel/side-panel-context";
import { useIsMobile } from "@/hooks/use-mobile";
import type { MCPSetupGuide } from "@gram/client/models/components/mcpsetupguide.js";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";

type SetupGuideLookup = {
  registrySpecifier?: string;
  serverUrl?: string;
};

type SetupGuideInput = SetupGuideLookup & {
  /**
   * The server's own icon, for the panel header. Carried alongside the lookup
   * rather than inside it: the guide catalog is indexed by endpoint and alias,
   * and this is only ever presentation.
   */
  iconUrl?: string;
};

export type SetupGuideMatch = {
  /**
   * The matched guide, or undefined when the two lookup keys matched different
   * guides. Each guide has its own docs page, so there is nowhere to link to in
   * that case, and nothing to name in the copy.
   */
  only: MCPSetupGuide | undefined;
  /**
   * Null on mobile, where the panel would have to split a viewport that has no
   * room to spare. Callers fall back to the docs site.
   */
  openGuide: (() => void) | null;
};

/**
 * Resolves the setup guide published for an MCP server, if there is one, and
 * the way to read it.
 *
 * Returns null in the common case of no guide, so callers can render nothing
 * without a lookup of their own. Either key may be omitted; supplying both
 * matches more servers, since some guides publish no registry alias and some
 * registry specifiers are not published as aliases.
 *
 * The presentation is left to callers — a page banner and a sidebar card want
 * different shapes — but the lookup, the mobile fallback, and the descriptor
 * handed to the panel are decided once, here.
 */
export function useSetupGuide({
  iconUrl,
  ...lookup
}: SetupGuideInput): SetupGuideMatch | null {
  const { openPanel } = useSidePanel();
  const isMobile = useIsMobile();

  // throwOnError: false because setup docs supplement whatever page hosts them,
  // so a failed lookup should leave that page intact rather than trip the error
  // boundary. No loading state either: most servers have no guide, so a
  // skeleton would flash and shift the layout on nearly every visit.
  const { data } = useGetMCPSetupDocs(lookup, undefined, {
    enabled: !!lookup.registrySpecifier || !!lookup.serverUrl,
    throwOnError: false,
  });

  const guides = data?.guides ?? [];
  if (guides.length === 0) return null;

  const only = soleGuide(guides);

  return {
    only,
    openGuide: isMobile
      ? null
      : () =>
          openPanel({
            kind: "setup-guide",
            ...setupGuideHeading(guides),
            iconUrl,
            // Each guide has its own docs page, so two matched guides leave the
            // header nowhere single to send anyone.
            docsUrl: only ? docsUrl(only) : undefined,
            // Serializable keys only: the panel outlives the page that opened
            // it, so it refetches rather than holding the loaded guides.
            props: lookup,
          }),
  };
}
