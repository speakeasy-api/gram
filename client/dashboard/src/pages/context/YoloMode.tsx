import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { useAutoPublish, type AutoPublishConfig } from "@/hooks/useAutoPublish";

export function YoloMode({ projectId }: { projectId: string }) {
  const { config, isLoading, setConfig } = useAutoPublish(projectId);

  if (isLoading || !config) {
    return null;
  }

  const handleToggle = (checked: boolean) => {
    setConfig({ ...config, enabled: checked });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Switch
          checked={config.enabled}
          onCheckedChange={handleToggle}
          aria-label="Auto-publish"
        />
        <Label>Auto-publish</Label>
      </div>

      {config.enabled && <ConfigPanel config={config} onSave={setConfig} />}
    </div>
  );
}

function ConfigPanel({
  config,
  onSave,
}: {
  config: AutoPublishConfig;
  onSave: (config: AutoPublishConfig) => void;
}) {
  const saveField = (field: keyof AutoPublishConfig, value: number) => {
    if ((config[field] as number) !== value) {
      onSave({ ...config, [field]: value });
    }
  };

  return (
    <div className="space-y-3 rounded-lg border border-border bg-card p-4">
      <div className="flex flex-col gap-1.5">
        <label htmlFor="yolo-interval" className="text-sm font-medium">
          Interval (minutes)
        </label>
        <input
          id="yolo-interval"
          type="number"
          min={1}
          defaultValue={config.intervalMinutes}
          onBlur={(e) => saveField("intervalMinutes", Number(e.target.value))}
          className="h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <label htmlFor="yolo-min-upvotes" className="text-sm font-medium">
          Minimum upvotes
        </label>
        <input
          id="yolo-min-upvotes"
          type="number"
          min={0}
          defaultValue={config.minUpvotes}
          onBlur={(e) => saveField("minUpvotes", Number(e.target.value))}
          className="h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
        />
      </div>
    </div>
  );
}
