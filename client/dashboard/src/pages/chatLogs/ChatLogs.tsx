import { LogsAgentsContent } from "@/components/observe/LogsAgents";

/**
 * Agent Sessions. The shell is the constrained ResourceListPage template
 * rather than the full-bleed workbench: the columns are a fixed set of short
 * values, so a viewport-wide table pushed all its slack into the session title
 * and left a hand's width of empty row beside it.
 *
 * The template is rendered by LogsAgentsContent, because every control it takes
 * — search, filters, sort, refresh — is derived from state that lives there.
 * This route contributes the scope it is gated on.
 */
export default function ChatLogs(): JSX.Element {
  return <LogsAgentsContent scope="org:admin" />;
}
