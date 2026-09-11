import { Badge } from "@/components/ui/Badge";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { Code, FileCode } from "lucide-react";
import type { SourceOption } from "./source-list";

/**
 * One source as a table row: the same source as `SourceCard`, cut for the
 * table view of the sources shelf. The whole row is the link to the source's
 * own page, so there is nothing to select here and no actions column.
 */
export function SourceTableRow({
  source,
  toolCount,
  href,
}: {
  source: SourceOption;
  /** Undefined while the tool list is still loading. */
  toolCount: number | undefined;
  href: string;
}): JSX.Element {
  const Icon = source.kind === "openapi" ? FileCode : Code;
  return (
    <DotRow
      icon={<Icon className="text-muted-foreground size-5" />}
      href={href}
      ariaLabel={`View source ${source.name}`}
    >
      <td className="px-3 py-3">
        <Text
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={source.name}
        >
          {source.name}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Badge variant="neutral">
          {source.kind === "openapi" ? "OpenAPI document" : "Function"}
        </Badge>
      </td>
      <td className="px-3 py-3">
        <Text small muted>
          {toolCount ?? "—"}
        </Text>
      </td>
    </DotRow>
  );
}
