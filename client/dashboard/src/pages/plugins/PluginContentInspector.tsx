import { CodeBlock } from "@/components/ui/code-block";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { usePluginPackageContentsSuspense } from "@gram/client/react-query/pluginPackageContents";
import type {
  PluginPackageContentsResult,
  PluginPackageFile,
} from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";

const PLATFORM_LABELS: Record<string, string> = {
  claude: "Claude Code",
  cursor: "Cursor",
  codex: "Codex",
};

function platformLabel(platform: string) {
  return PLATFORM_LABELS[platform] ?? platform;
}

export function PluginContentInspector({ pluginId }: { pluginId: string }) {
  const { data } = usePluginPackageContentsSuspense({ pluginId });
  return <InspectorBody data={data} />;
}

function InspectorBody({ data }: { data: PluginPackageContentsResult }) {
  const platforms = data.platforms;
  const defaultPlatform = platforms[0]?.platform ?? "claude";

  if (platforms.length === 0) {
    return (
      <Type muted small>
        No platform contents to display.
      </Type>
    );
  }

  return (
    <Stack gap={3}>
      <Type muted small>
        API keys are minted at download or publish time. Tokens shown here are
        replaced with{" "}
        <code className="bg-muted rounded px-1 py-0.5 text-xs">
          {data.redactedKeyPlaceholder}
        </code>{" "}
        and cannot be used to authenticate.
      </Type>
      <Tabs defaultValue={defaultPlatform}>
        <TabsList>
          {platforms.map((p) => (
            <TabsTrigger key={p.platform} value={p.platform}>
              {platformLabel(p.platform)}
            </TabsTrigger>
          ))}
        </TabsList>
        {platforms.map((p) => (
          <TabsContent key={p.platform} value={p.platform}>
            <PlatformFiles files={p.files} />
          </TabsContent>
        ))}
      </Tabs>
    </Stack>
  );
}

function PlatformFiles({ files }: { files: PluginPackageFile[] }) {
  const sortedFiles = useMemo(
    () => [...files].sort((a, b) => a.path.localeCompare(b.path)),
    [files],
  );
  const [selectedPath, setSelectedPath] = useState<string>(
    sortedFiles[0]?.path ?? "",
  );
  const selected = sortedFiles.find((f) => f.path === selectedPath);

  if (sortedFiles.length === 0) {
    return (
      <Type muted small>
        No files generated for this platform.
      </Type>
    );
  }

  return (
    <div className="grid grid-cols-[minmax(180px,260px)_1fr] gap-3">
      <ul className="border-border max-h-[480px] overflow-y-auto rounded-md border">
        {sortedFiles.map((f) => (
          <li key={f.path}>
            <button
              type="button"
              onClick={() => setSelectedPath(f.path)}
              className={cn(
                "hover:bg-muted/60 w-full truncate px-3 py-2 text-left font-mono text-xs",
                f.path === selectedPath && "bg-muted text-foreground",
              )}
              title={f.path}
            >
              {f.path}
            </button>
          </li>
        ))}
      </ul>
      <div className="min-w-0">
        {selected && (
          <CodeBlock
            key={selected.path}
            title={selected.path}
            content={selected.contents}
          />
        )}
      </div>
    </div>
  );
}
