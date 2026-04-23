import {
  invalidateAllListSkills,
  invalidateAllSkillsListPending,
  invalidateAllSkillsListVersions,
  useSkillsApproveVersionMutation,
  useSkillsListPending,
  useSkillsListVersions,
  useSkillsRejectVersionMutation,
} from "@gram/client/react-query";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { RequireScope } from "@/components/require-scope";
import { SkillUploadDialog } from "@/pages/skills/components/SkillUploadDialog";
import { SkillVersionDiffPanel } from "@/pages/skills/components/SkillVersionDiffPanel";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { formatBytes } from "@/lib/format-bytes";
import { toast } from "sonner";
import { useProject } from "@/contexts/Auth";
import { useQueryClient } from "@tanstack/react-query";
import { useRoutes } from "@/routes";

export default function SkillsReview() {
  return (
    <div className="p-8">
      <div className="mx-auto max-w-6xl">
        <RequireScope scope="project:read" level="page">
          <SkillsReviewInner />
        </RequireScope>
      </div>
    </div>
  );
}

function SkillsReviewInner() {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const project = useProject();
  const { data, isPending, error } = useSkillsListPending();

  const [rejectDialog, setRejectDialog] = useState<{
    open: boolean;
    versionId: string;
    skillName: string;
  }>({
    open: false,
    versionId: "",
    skillName: "",
  });
  const [rejectReason, setRejectReason] = useState("");

  const approveMutation = useSkillsApproveVersionMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllSkillsListPending(queryClient),
        invalidateAllListSkills(queryClient),
        invalidateAllSkillsListVersions(queryClient),
      ]);
      toast.success("Version approved");
    },
    onError: () => {
      toast.error("Failed to approve version");
    },
  });

  const rejectMutation = useSkillsRejectVersionMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllSkillsListPending(queryClient),
        invalidateAllListSkills(queryClient),
        invalidateAllSkillsListVersions(queryClient),
      ]);
      toast.success("Version rejected");
      setRejectDialog({ open: false, versionId: "", skillName: "" });
      setRejectReason("");
    },
    onError: () => {
      toast.error("Failed to reject version");
    },
  });

  const rows = useMemo(() => {
    return (data?.skills ?? []).flatMap((entry) =>
      entry.versions.map((version) => ({
        skillId: entry.skill.id,
        skillSlug: entry.skill.slug,
        skillName: entry.skill.name,
        versionId: version.id,
        state: version.state,
        author: version.authorName || "Unknown",
        sizeBytes: version.sizeBytes,
        createdAt: version.createdAt,
        assetId: version.assetId,
        activeVersionId: entry.skill.activeVersionId ?? null,
      })),
    );
  }, [data?.skills]);

  const [selectedVersionId, setSelectedVersionId] = useState<string | null>(
    null,
  );

  const selectedRow =
    rows.find((row) => row.versionId === selectedVersionId) ?? rows[0] ?? null;

  const selectedSkillVersionsQuery = useSkillsListVersions(
    {
      skillId: selectedRow?.skillId ?? "",
    },
    undefined,
    {
      enabled: Boolean(selectedRow?.skillId && selectedRow?.activeVersionId),
    },
  );

  const selectedSkillActiveVersion = useMemo(() => {
    if (!selectedRow?.activeVersionId) {
      return null;
    }

    return (
      selectedSkillVersionsQuery.data?.versions.find(
        (version) => version.id === selectedRow.activeVersionId,
      ) ?? null
    );
  }, [selectedRow?.activeVersionId, selectedSkillVersionsQuery.data?.versions]);

  const shouldWaitForBaseline = Boolean(
    selectedRow?.activeVersionId &&
    selectedRow.activeVersionId !== selectedRow.versionId &&
    selectedSkillVersionsQuery.isPending,
  );

  const handleApprove = (versionId: string) => {
    approveMutation.mutate({
      request: {
        approveVersionRequestBody: { versionId },
      },
    });
  };

  const openRejectDialog = (versionId: string, skillName: string) => {
    setRejectReason("");
    setRejectDialog({ open: true, versionId, skillName });
  };

  const handleReject = () => {
    if (!rejectReason.trim()) {
      toast.error("Please provide a reject reason");
      return;
    }

    rejectMutation.mutate({
      request: {
        rejectVersionRequestBody: {
          versionId: rejectDialog.versionId,
          reason: rejectReason.trim(),
        },
      },
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Type variant="subheading">Review</Type>
          <Type small muted className="mt-1 block max-w-3xl">
            Review pending captured or uploaded versions before they become
            active.
          </Type>
        </div>
        <RequireScope scope="project:write" level="component">
          <SkillUploadDialog
            onUploaded={async () => {
              await Promise.all([
                invalidateAllSkillsListPending(queryClient),
                invalidateAllListSkills(queryClient),
                invalidateAllSkillsListVersions(queryClient),
              ]);
            }}
          />
        </RequireScope>
      </div>

      {error ? (
        <div className="rounded-xl border border-dashed px-8 py-16 text-center">
          <Type variant="subheading" className="mb-1">
            Couldn&apos;t load pending versions
          </Type>
          <Type small muted>
            There was a problem loading the review queue for this project.
          </Type>
        </div>
      ) : isPending ? (
        <div className="rounded-xl border border-dashed px-8 py-16 text-center">
          <Type small muted>
            Loading pending versions…
          </Type>
        </div>
      ) : rows.length === 0 ? (
        <div className="rounded-xl border border-dashed px-8 py-16 text-center">
          <Type variant="subheading" className="mb-1">
            No pending versions
          </Type>
          <Type small muted>
            New captures and uploads pending review will appear here.
          </Type>
        </div>
      ) : (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
          <DotTable
            headers={[
              { label: "Skill" },
              { label: "Version" },
              { label: "Author" },
              { label: "Captured" },
              { label: "Size" },
              { label: "Actions" },
            ]}
          >
            {rows.map((row) => (
              <DotRow
                key={row.versionId}
                onClick={() => setSelectedVersionId(row.versionId)}
                className={
                  selectedRow?.versionId === row.versionId
                    ? "bg-muted/30"
                    : undefined
                }
              >
                <td className="px-3 py-3">
                  <button
                    type="button"
                    className="text-left"
                    onClick={(event) => {
                      event.stopPropagation();
                      routes.skills.registry.skill.goTo(
                        row.skillSlug,
                        "versions",
                      );
                    }}
                  >
                    <Type variant="subheading" as="div" className="text-sm">
                      {row.skillName}
                    </Type>
                    <Type small muted className="font-mono">
                      {row.skillSlug}
                    </Type>
                  </button>
                </td>
                <td className="px-3 py-3">
                  <Type small className="font-mono">
                    {row.versionId.slice(0, 8)}
                  </Type>
                  <Type small muted className="block capitalize">
                    {row.state.replace("_", " ")}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <Type small muted>
                    {row.author}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <Type small muted>
                    {formatDateTime(row.createdAt)}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <Type small muted>
                    {formatBytes(row.sizeBytes)}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <div className="flex items-center gap-2">
                    <RequireScope scope="project:write" level="component">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={
                          approveMutation.isPending || rejectMutation.isPending
                        }
                        onClick={(event) => {
                          event.stopPropagation();
                          handleApprove(row.versionId);
                        }}
                      >
                        Approve
                      </Button>
                    </RequireScope>
                    <RequireScope scope="project:write" level="component">
                      <Button
                        size="sm"
                        variant="destructiveGhost"
                        disabled={
                          approveMutation.isPending || rejectMutation.isPending
                        }
                        onClick={(event) => {
                          event.stopPropagation();
                          openRejectDialog(row.versionId, row.skillName);
                        }}
                      >
                        Reject
                      </Button>
                    </RequireScope>
                  </div>
                </td>
              </DotRow>
            ))}
          </DotTable>

          <SkillVersionDiffPanel
            projectId={project.id}
            target={
              selectedRow && !shouldWaitForBaseline
                ? {
                    versionId: selectedRow.versionId,
                    assetId: selectedRow.assetId,
                    label: `${selectedRow.skillName} (${selectedRow.versionId.slice(0, 8)})`,
                  }
                : null
            }
            baseline={
              selectedRow?.activeVersionId &&
              selectedRow.activeVersionId !== selectedRow.versionId &&
              selectedSkillVersionsQuery.isSuccess
                ? {
                    versionId: selectedRow.activeVersionId,
                    assetId: selectedSkillActiveVersion?.assetId,
                    label: "Active version",
                  }
                : null
            }
          />
        </div>
      )}

      <Dialog
        open={rejectDialog.open}
        onOpenChange={(open) => {
          if (!open) {
            setRejectDialog({ open: false, versionId: "", skillName: "" });
            setRejectReason("");
          }
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Reject version</Dialog.Title>
            <Dialog.Description>
              Provide a reason for rejecting this version of{" "}
              <strong>{rejectDialog.skillName}</strong>.
            </Dialog.Description>
          </Dialog.Header>

          <TextArea
            value={rejectReason}
            onChange={setRejectReason}
            rows={5}
            placeholder="Explain why this version should not be approved"
          />

          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() =>
                setRejectDialog({ open: false, versionId: "", skillName: "" })
              }
              disabled={rejectMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleReject}
              disabled={!rejectReason.trim() || rejectMutation.isPending}
            >
              {rejectMutation.isPending ? "Rejecting…" : "Reject version"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function formatDateTime(date: Date) {
  return new Intl.DateTimeFormat("en-GB", {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
