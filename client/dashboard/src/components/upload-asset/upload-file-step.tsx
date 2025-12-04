import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { OpenApiSourceInput } from "../OpenApiSourceInput";
import { Type } from "../ui/type";
import { useStep } from "./step";
import { useStepper } from "./stepper";

export default function UploadFileStep() {
  const project = useProject();
  const session = useSession();
  const stepper = useStepper();
  const step = useStep();

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
      step.setState("failed");
      throw new Error(`Upload failed`);
    }

    stepper.meta.current.uploadResult = await response.json();
    stepper.meta.current.file = uploadingFile;
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
    return <OpenApiSourceInput onUpload={handleUpload} />;
  }
}
