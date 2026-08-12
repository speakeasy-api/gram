import { Expandable } from "@/components/expandable";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { FileJson2, X } from "lucide-react";
import { useEffect, useState } from "react";

export const UploadedDocument = ({
  file,
  onReset,
  defaultExpanded = false,
}: {
  file: File;
  onReset: () => void;
  defaultExpanded?: boolean;
}): JSX.Element => {
  const [fileText, setFileText] = useState<string>();

  useEffect(() => {
    if (!file) return;
    if (file.size > 10_000) {
      void file
        .slice(0, 10_000)
        .text()
        .then((text) => setFileText(text + "\n..."));
    } else {
      void file.text().then(setFileText);
    }
  }, [file]);

  return (
    <Expandable defaultExpanded={defaultExpanded}>
      <Expandable.Trigger>
        <Stack direction={"horizontal"} gap={2} align={"center"}>
          <FileJson2 className="text-muted-foreground/70 h-4 w-4" />
          <Text small mono>
            {file.name}
          </Text>
          <Button
            variant="tertiary"
            onClick={onReset}
            className="size-6 opacity-50 hover:opacity-100"
          >
            <Button.LeftIcon>
              <X className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text className="sr-only">Remove file</Button.Text>
          </Button>
        </Stack>
      </Expandable.Trigger>
      <Expandable.Content className="text-xs">
        {fileText?.length ? (
          <pre className="break-all whitespace-pre-wrap">{fileText}</pre>
        ) : (
          <SkeletonParagraph lines={12} />
        )}
      </Expandable.Content>
    </Expandable>
  );
};
