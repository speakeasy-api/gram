import { Icon, Stack } from "@speakeasy-api/moonshine";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Type } from "@/components/ui/type";
import { useListReleases } from "@gram/client/react-query";
import { formatDistanceToNow } from "date-fns";
import { CreateReleaseDialog } from "./CreateReleaseDialog";
import { RollbackDialog } from "./RollbackDialog";

interface ReleasesTabProps {
  toolsetSlug: string;
  editingMode: string;
}

export const ReleasesTab = ({ toolsetSlug, editingMode }: ReleasesTabProps) => {
  const { data: releasesResult, refetch } = useListReleases({
    toolsetSlug,
    limit: 50,
  });

  const releases = releasesResult?.releases ?? [];
  const isStaging = editingMode === "staging";

  if (!isStaging) {
    return (
      <Stack gap={4} className="py-8" align="center">
        <Icon name="info" size="large" className="text-muted-foreground" />
        <Stack gap={2} align="center">
          <Type variant="h3">Releases require staging mode</Type>
          <Type variant="body" className="text-muted-foreground text-center">
            Switch to staging mode to create and manage releases for this
            toolset.
          </Type>
        </Stack>
      </Stack>
    );
  }

  return (
    <Stack gap={4}>
      <Stack direction="horizontal" justify="space-between" align="center">
        <Stack gap={1}>
          <Type variant="h3">Releases</Type>
          <Type variant="body" className="text-muted-foreground">
            {releases.length === 0
              ? "No releases yet"
              : `${releases.length} release${releases.length === 1 ? "" : "s"}`}
          </Type>
        </Stack>
        <CreateReleaseDialog
          toolsetSlug={toolsetSlug}
          onReleaseCreated={refetch}
        />
      </Stack>

      {releases.length === 0 ? (
        <Stack gap={4} className="py-12" align="center">
          <Icon name="package" size="large" className="text-muted-foreground" />
          <Stack gap={2} align="center">
            <Type variant="h4">No releases yet</Type>
            <Type variant="body" className="text-muted-foreground text-center">
              Create your first release to publish the current staging state.
            </Type>
          </Stack>
        </Stack>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Release</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Notes</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {releases.map((release) => (
              <TableRow key={release.id}>
                <TableCell>
                  <Stack direction="horizontal" gap={2} align="center">
                    <Icon name="package" size="small" />
                    <Type variant="label">#{release.releaseNumber}</Type>
                  </Stack>
                </TableCell>
                <TableCell>
                  <Type variant="body" className="text-muted-foreground">
                    {formatDistanceToNow(release.createdAt, {
                      addSuffix: true,
                    })}
                  </Type>
                </TableCell>
                <TableCell>
                  {release.notes ? (
                    <Type variant="body" className="truncate max-w-md">
                      {release.notes}
                    </Type>
                  ) : (
                    <Type
                      variant="body"
                      className="text-muted-foreground italic"
                    >
                      No notes
                    </Type>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <RollbackDialog
                    toolsetSlug={toolsetSlug}
                    releaseNumber={release.releaseNumber}
                    onRollbackComplete={refetch}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Stack>
  );
};
