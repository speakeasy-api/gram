import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { Badge } from "@/components/ui/Badge";
import { DotRow } from "@/components/ui/DotRow";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { Code, FileCode } from "lucide-react";
import type { SourceOption } from "./source-list";
import { SourceFailureNotice } from "./source-list-notices";

// Shared by every cell so the row's rhythm is set in one place.
const CELL = "px-3 py-3";

function DateCell({ date }: { date: Date | undefined }): JSX.Element {
  return (
    <td className={CELL}>
      <Text small muted>
        {date ? <HumanizeDateTime date={date} includeTime={false} /> : "—"}
      </Text>
    </td>
  );
}

/**
 * One source as a table row: the same source as `SourceCard`, cut for the
 * table view of the sources shelf. The whole row is the link to the source's
 * own page; the kebab and the right-click menu carry the same actions.
 */
export function SourceTableRow({
  source,
  toolCount,
  href,
  actions = [],
  failedDeploymentId,
}: {
  source: SourceOption;
  /** Undefined while the tool list is still loading. */
  toolCount: number | undefined;
  href: string;
  actions?: Action[];
  /** Set when this source caused the latest deployment to fail. */
  failedDeploymentId?: string | undefined;
}): JSX.Element {
  const Icon = source.kind === "openapi" ? FileCode : Code;
  const failing = failedDeploymentId !== undefined;
  return (
    <TableRowContextMenu actions={actions}>
      <DotRow
        icon={<Icon className="text-muted-foreground size-5" />}
        href={href}
        ariaLabel={`View source ${source.name}`}
      >
        <td className={CELL}>
          <Text
            variant="subheading"
            as="div"
            className="group-hover:text-primary truncate text-sm transition-colors"
            title={source.name}
          >
            {source.name}
          </Text>
        </td>
        <td className={CELL}>
          <Badge variant="neutral">
            {source.kind === "openapi" ? "OpenAPI document" : "Function"}
          </Badge>
        </td>
        <td className={CELL}>
          <Text small muted>
            {toolCount ?? "—"}
          </Text>
        </td>
        <DateCell date={source.createdAt} />
        <DateCell date={source.updatedAt} />
        <td className={CELL}>
          {failing && (
            <div className="text-destructive relative z-20 flex items-center gap-1.5">
              <SourceFailureNotice
                deploymentId={failedDeploymentId}
                className="flex items-center"
              />
              <Text small className="text-destructive">
                Error
              </Text>
            </div>
          )}
        </td>
        <td className={CELL}>
          {actions.length > 0 && (
            // Above the row's stretched link, or the menu button would open
            // the source page instead.
            <div className="relative z-20 flex items-center justify-end">
              <MoreActions actions={actions} />
            </div>
          )}
        </td>
      </DotRow>
    </TableRowContextMenu>
  );
}
