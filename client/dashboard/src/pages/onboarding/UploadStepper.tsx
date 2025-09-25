type StepStatus = "idle" | "pending" | "complete" | "error";

type StepperContext = {};

interface UploadStepProps {
  next: () => void;
  status: "idle" | "uploading" | "processing" | "error" | "complete";
}
