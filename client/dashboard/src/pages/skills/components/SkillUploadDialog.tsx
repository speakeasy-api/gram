import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  UploadManualSkillHeaderXGramSkillAssetFormat,
  UploadManualSkillHeaderXGramSkillDiscoveryRoot,
  UploadManualSkillHeaderXGramSkillResolutionStatus,
  UploadManualSkillHeaderXGramSkillScope,
  UploadManualSkillHeaderXGramSkillSourceType,
} from "@gram/client/models/operations/uploadmanualskill";
import {
  useListSkills,
  useSkillsUploadManualMutation,
} from "@gram/client/react-query";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FullWidthUpload } from "@/components/upload";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { formatBytes } from "@/lib/format-bytes";
import { toast } from "sonner";

type LineageMode = "new" | "existing";

export function SkillUploadDialog({
  onUploaded,
}: {
  onUploaded?: () => Promise<void> | void;
}) {
  const { data: skillsData, isPending: areSkillsPending } = useListSkills();

  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [skillName, setSkillName] = useState("");
  const [lineageMode, setLineageMode] = useState<LineageMode>("new");
  const [existingSkillId, setExistingSkillId] = useState("");
  const [isPreparing, setIsPreparing] = useState(false);

  const skills = useMemo(() => {
    return [...(skillsData?.skills ?? [])].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }, [skillsData?.skills]);

  const uploadMutation = useSkillsUploadManualMutation({
    onSuccess: async () => {
      await onUploaded?.();
      toast.success("Skill uploaded for review");
      resetAndClose();
    },
    onError: () => {
      toast.error("Failed to upload skill");
    },
  });

  const handleFileUpload = (nextFile: File) => {
    setFile(nextFile);
    if (!skillName.trim()) {
      setSkillName(nextFile.name.replace(/\.zip$/i, ""));
    }
  };

  const handleSubmit = async () => {
    if (!file) {
      toast.error("Select a ZIP file to upload");
      return;
    }
    if (!skillName.trim()) {
      toast.error("Enter a skill name");
      return;
    }
    if (lineageMode === "existing" && !existingSkillId) {
      toast.error("Select an existing skill lineage");
      return;
    }

    setIsPreparing(true);
    try {
      const contentSha256 = await hashFileSha256(file);
      uploadMutation.mutate({
        request: {
          xGramSkillName: skillName.trim(),
          xGramSkillScope: UploadManualSkillHeaderXGramSkillScope.Project,
          xGramSkillDiscoveryRoot:
            UploadManualSkillHeaderXGramSkillDiscoveryRoot.ManualUpload,
          xGramSkillSourceType:
            UploadManualSkillHeaderXGramSkillSourceType.ManualUpload,
          xGramSkillContentSha256: contentSha256,
          xGramSkillAssetFormat:
            UploadManualSkillHeaderXGramSkillAssetFormat.Zip,
          xGramSkillResolutionStatus:
            UploadManualSkillHeaderXGramSkillResolutionStatus.Resolved,
          ...(lineageMode === "existing"
            ? { xGramSkillId: existingSkillId }
            : {}),
          contentType: file.type || "application/zip",
          contentLength: file.size,
          requestBody: file,
        },
      });
    } catch {
      toast.error("Failed to prepare upload");
    } finally {
      setIsPreparing(false);
    }
  };

  const isPending = uploadMutation.isPending || isPreparing;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (isPending) {
          return;
        }
        if (!nextOpen) {
          resetForm();
        }
        setOpen(nextOpen);
      }}
    >
      <Dialog.Trigger asChild>
        <Button variant="outline" size="sm" icon="upload">
          Upload skill
        </Button>
      </Dialog.Trigger>

      <Dialog.Content className="sm:max-w-xl">
        <Dialog.Header>
          <Dialog.Title>Upload skill ZIP</Dialog.Title>
          <Dialog.Description>
            Upload a packaged skill version and add it to review. Choose whether
            this creates a new lineage or attaches to an existing one.
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label>Skill package</Label>
            <FullWidthUpload
              onUpload={handleFileUpload}
              allowedExtensions={["zip"]}
              isLoading={isPending}
              label={
                <>
                  <span className="font-semibold">Click to upload</span> or drag
                  and drop a skill ZIP
                </>
              }
            />
            {file ? (
              <Type small muted>
                Selected: <span className="font-mono">{file.name}</span> (
                {formatBytes(file.size)})
              </Type>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="skill-upload-name">Display name</Label>
            <Input
              id="skill-upload-name"
              placeholder="e.g. golang"
              value={skillName}
              onChange={setSkillName}
            />
          </div>

          <div className="space-y-2">
            <Label>Lineage</Label>
            <Select
              value={lineageMode}
              onValueChange={(value) => setLineageMode(value as LineageMode)}
              disabled={isPending}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="new">Create new skill lineage</SelectItem>
                <SelectItem value="existing">
                  Attach to existing lineage
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          {lineageMode === "existing" ? (
            <div className="space-y-2">
              <Label>Existing skill</Label>
              <Select
                value={existingSkillId}
                onValueChange={setExistingSkillId}
                disabled={isPending || areSkillsPending || skills.length === 0}
              >
                <SelectTrigger className="w-full">
                  <SelectValue
                    placeholder={
                      areSkillsPending
                        ? "Loading skills…"
                        : skills.length === 0
                          ? "No existing skills found"
                          : "Select a skill"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {skills.map((skill) => (
                    <SelectItem key={skill.id} value={skill.id}>
                      {skill.name} ({skill.slug})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}
        </div>

        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={isPending}
            onClick={resetAndClose}
          >
            Cancel
          </Button>
          <Button disabled={isPending} onClick={handleSubmit}>
            {isPreparing
              ? "Preparing…"
              : uploadMutation.isPending
                ? "Uploading…"
                : "Upload"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );

  function resetAndClose() {
    resetForm();
    setOpen(false);
  }

  function resetForm() {
    setFile(null);
    setSkillName("");
    setLineageMode("new");
    setExistingSkillId("");
  }
}

async function hashFileSha256(file: File): Promise<string> {
  const buffer = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  return Array.from(new Uint8Array(digest))
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}
