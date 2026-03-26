import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { toast } from "sonner";
import { OpenApiSourceInput } from "../OpenApiSourceInput";
import { Type } from "../ui/type";
import { useStep } from "./step";
import { useStepper } from "./stepper";
import type { UploadOpenAPIv3Result } from "@gram/client/models/components";

export default function UploadFileStep() {
  const project = useProject();
  const session = useSession();
  const stepper = useStepper();
  const step = useStep();
  const [isUploading, setIsUploading] = useState(false);

  const getContentType = (file: File) => {
    if (file.type) return file.type;
    const ext = file.name.split(".").pop()?.toLowerCase();
    switch (ext) {
      case "json":
        return "application/json";
      case "yaml":
      case "yml":
        return "application/yaml";
      default:
        return "application/octet-stream";
    }
  };

  async function handleUpload(uploadingFile: File) {
    setIsUploading(true);
    try {
      const response = await fetch(
        `${getServerURL()}/rpc/assets.uploadOpenAPIv3`,
        {
          method: "POST",
          headers: {
            "content-type": getContentType(uploadingFile),
            "content-length": uploadingFile.size.toString(),
            "gram-session": session.session,
            "gram-project": project.slug,
          },
          body: uploadingFile,
        },
      );

      if (!response.ok) {
        throw new Error(`Upload failed`);
      }

      stepper.meta.current.uploadResult = await response.json();
      stepper.meta.current.file = uploadingFile;
      stepper.next();
      step.setState("completed");
    } catch (_error) {
      step.setState("failed");
      toast.error("Failed to upload OpenAPI spec");
    } finally {
      setIsUploading(false);
    }
  }

  function handleUrlUpload(result: UploadOpenAPIv3Result) {
    stepper.meta.current.uploadResult = result;
    stepper.meta.current.file = new File([], "My API", {
      type: result.asset?.contentType ?? "application/yaml",
    });
    stepper.next();
    step.setState("completed");
  }

  if (stepper.meta.current.file) {
    return (
      <Stack direction={"horizontal"} gap={2} align={"center"}>
        <Type>✓ Uploaded {stepper.meta.current.file.name}</Type>
      </Stack>
    );
  } else {
    return (
      <OpenApiSourceInput
        onUpload={handleUpload}
        onUrlUpload={handleUrlUpload}
        isLoading={isUploading}
      />
    );
  }
}
