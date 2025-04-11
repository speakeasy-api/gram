import { useState } from "react";
import { Button } from "@/components/ui/button";
import { UploadIcon } from "lucide-react";
import { cn } from "@/lib/utils";
export default function FileUpload({
  onUpload,
  className,
}: {
  onUpload: (file: File) => void;
  className?: string;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [isInvalidFile, setIsInvalidFile] = useState(false);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    console.log("File changed:", e.target.files?.[0]);
    setFile(e.target.files?.[0] ?? null);
  };

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (file) {
      onUpload(file);
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className={cn("grid gap-4 w-full max-w-2xl", className)}
      onDragOver={(e) => {
        e.preventDefault();
        const items = e.dataTransfer.items;
        if (items?.[0]) {
          const fileExtension =
            items[0].type === "application/json" ||
            items[0].type === "application/x-yaml" ||
            items[0].type === "text/yaml";
          setIsInvalidFile(!fileExtension);
        }
      }}
      onDragLeave={() => setIsInvalidFile(false)}
      onDrop={(e) => {
        e.preventDefault();
        setIsInvalidFile(false);
        const droppedFile = e.dataTransfer.files[0];
        if (droppedFile) {
          const fileExtension = droppedFile.name.toLowerCase().split(".").pop();
          if (["json", "yaml", "yml"].includes(fileExtension ?? "")) {
            setFile(droppedFile);
          } else {
            console.warn(
              "Invalid file type. Please upload a JSON or YAML file.",
            );
          }
        }
      }}
    >
      <div className="flex items-center justify-center w-full">
        <label
          htmlFor="dropzone-file"
          className={`flex flex-col items-center justify-center w-full h-64 border-2 border-dashed rounded-lg cursor-pointer bg-card trans ${
            isInvalidFile
              ? "border-destructive bg-destructive/10"
              : "border-muted-foreground/50 hover:bg-input"
          }`}
        >
          <div className="flex flex-col items-center justify-center pt-5 pb-6">
            <UploadIcon className="w-8 h-8 text-muted-foreground" />
            <p className="my-2 text-sm text-card-foreground">
              <span className="font-semibold">Click to upload</span> or drag and
              drop
            </p>
            <p className="text-xs text-muted-foreground">
              OpenAPI YAML or JSON (max 8MiB)
            </p>
          </div>
          <input
            id="dropzone-file"
            type="file"
            className="hidden"
            onChange={handleFileChange}
            accept=".json,.yaml,.yml"
          />
        </label>
      </div>
      {file && (
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{file.name}</p>
            <p className="text-sm text-muted-foreground">
              {(file.size / 1024).toFixed(2)} KB
            </p>
          </div>
          <Button type="submit" className="cursor-pointer">
            Upload
          </Button>
        </div>
      )}
    </form>
  );
}
