import { TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { MCPApprovalsTab } from "@/pages/security/MCPApprovalsTab";
import { ShadowAITargetsTab } from "@/pages/security/ShadowAITargetsTab";
import { useShadowAIPolicySlot } from "@/pages/security/use-shadow-ai-policy-slot";
import { useRoutes } from "@/routes";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import { Link, Navigate, Outlet, useLocation, useParams } from "react-router";
import {
  SHADOW_AI_TABS,
  type ShadowAITab,
  shadowAITabs,
  shadowAITabSearch,
} from "./tabs";

export function ShadowAIRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * Shadow AI: the MCP servers and AI tools people use that nobody has
 * approved yet, and the one policy that decides which of them may connect.
 * It sits beside Guardrails rather than inside it: Guardrails holds the
 * policies that scan agent sessions for risk, while this page is an inventory
 * the admin decides on row by row.
 *
 * The inventory reads and every decision require org:admin, so the page is
 * gated there, the same as Guardrails.
 */
export default function ShadowAI(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ShadowAIContent />
    </RequireScope>
  );
}

function ShadowAIContent(): JSX.Element {
  const [activeTab] = useQueryState(
    "tab",
    parseAsStringLiteral(SHADOW_AI_TABS).withDefault("mcp-servers"),
  );
  return (
    <TabbedPage
      title="Shadow AI"
      description="See the MCP servers and AI tools your team uses, and decide which may connect."
      primaryAction={<ShadowAIPolicyAction />}
      activeTab={activeTab}
      tabs={shadowAITabs()}
    >
      {activeTab === "mcp-servers" && <MCPApprovalsTab />}
      {activeTab === "ai-tools" && <ShadowAITargetsTab />}
    </TabbedPage>
  );
}

/** The project has one Shadow AI policy. The header opens it, or the editor
 *  that creates it, so setting it up no longer starts from Guardrails. */
function ShadowAIPolicyAction(): JSX.Element | null {
  const routes = useRoutes();
  const slot = useShadowAIPolicySlot();
  if (slot.pending || slot.failed) return null;
  if (slot.href) {
    return (
      <Button variant="secondary" asChild>
        <Link to={slot.href}>
          <Button.Text>Edit policy</Button.Text>
        </Link>
      </Button>
    );
  }
  return (
    <Button asChild>
      <Link to={`${routes.policyCenter.new.href()}?kind=shadow-ai`}>
        <Button.Text>Set up policy</Button.Text>
      </Link>
    </Button>
  );
}

function ShadowAITabRedirect({ tab }: { tab: ShadowAITab }): JSX.Element {
  const routes = useRoutes();
  return (
    <Navigate
      to={`${routes.shadowAI.href()}${shadowAITabSearch(tab)}`}
      replace
    />
  );
}

// The category indexes (harnesses, assistants, models) never had a page of
// their own after the tools moved into one list; links to them land on the
// AI tools tab. Their tool detail pages beneath still render.
export function ShadowAIToolsIndexRedirect(): JSX.Element {
  return <ShadowAITabRedirect tab="ai-tools" />;
}

// The MCPs index of the section and the older Shadow MCP index were the
// approvals queue. Block messages, emails and bookmarks carry both paths, so
// they land on the MCP servers tab. Their query and fragment named filters of
// the old page, so they are dropped rather than carried over.
export function ShadowMCPIndexRedirect(): JSX.Element {
  return <ShadowAITabRedirect tab="mcp-servers" />;
}

export function ShadowMCPServerLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { serverSlug = "" } = useParams<{ serverSlug: string }>();
  return (
    <Navigate
      to={`${routes.shadowAI.mcps.detail.href(serverSlug)}${location.search}${location.hash}`}
      replace
    />
  );
}
