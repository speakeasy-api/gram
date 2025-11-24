import {
  Badge,
  Button,
  Heading,
  Text,
  useModal,
  Skeleton,
  Icon,
} from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";
import { format } from "date-fns";
import {
  fetchChangelog,
  formatChangelogEntry,
  type ChangelogEntry
} from "@/services/changelog";

export function ChangelogModal({ onClose }: { onClose?: () => void }) {
  const { close } = useModal();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [entries, setEntries] = useState<ChangelogEntry[]>([]);

  useEffect(() => {
    loadChangelog();
  }, []);

  const loadChangelog = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetchChangelog(5);
      setEntries(response.entries);
    } catch (err) {
      console.error("Error fetching changelog:", err);
      setError("Unable to load changelog at this time");
    } finally {
      setLoading(false);
    }
  };

  const handleClose = () => {
    close();
    onClose?.();
  };


  return (
    <div className="flex flex-col p-6 max-h-[80vh] w-full max-w-4xl">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-xl bg-gradient-to-br from-brand-500/10 to-brand-600/10 border border-brand-500/20">
            <Icon name="rocket" className="size-8 text-brand-500" />
          </div>
          <div className="flex-1">
            <Heading className="flex items-center gap-3 text-xl">
              What's new in Gram
              <Badge variant="success" className="text-sm px-3 py-1">
                v0.10.3
              </Badge>
            </Heading>
            <Text className="text-muted-foreground text-base mt-2">
              Latest updates and features for the Gram MCP server platform
            </Text>
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto min-h-0">
        {loading ? (
          <div className="space-y-4 w-full">
            {[1, 2, 3].map((i) => (
              <div key={i} className="p-4 rounded-lg border border-neutral-200 dark:border-neutral-800">
                <Skeleton className="h-5 w-48 mb-2">
                  <div className="h-5 w-48" />
                </Skeleton>
                <Skeleton className="h-4 w-full mb-1">
                  <div className="h-4 w-full" />
                </Skeleton>
                <Skeleton className="h-4 w-3/4">
                  <div className="h-4 w-3/4" />
                </Skeleton>
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="flex flex-col items-center justify-center py-12">
            <Icon name="circle-alert" className="size-12 text-muted-foreground mb-3" />
            <Text className="text-muted-foreground">{error}</Text>
            <Button variant="tertiary" onClick={loadChangelog} className="mt-3">
              <Icon name="refresh-cw" className="size-4" />
              Try again
            </Button>
          </div>
        ) : (
          <div className="space-y-4 w-full">
            {entries.map((entry, index) => {
              const formatting = formatChangelogEntry(entry);
              return (
                <div
                  key={entry.id}
                  className="group py-6 px-8 rounded-xl border border-neutral-200 dark:border-neutral-800 hover:border-neutral-300 dark:hover:border-neutral-700 hover:bg-neutral-50 dark:hover:bg-neutral-900/50 transition-all w-full"
                >
                  <div className="flex items-start gap-4 w-full">
                    <div className="text-muted-foreground flex-shrink-0 mt-1">
                      <Icon name={formatting.icon as any} className="size-5" />
                    </div>
                    <div className="flex-1 w-full">
                      <div className="flex items-center gap-3 mb-3">
                        <Text className="font-semibold text-lg">
                          {entry.title}
                        </Text>
                        {index === 0 && (
                          <Badge variant={formatting.badgeVariant} className="text-sm px-2 py-0.5">
                            {formatting.typeLabel}
                          </Badge>
                        )}
                      </div>
                      <Text className="text-base text-muted-foreground leading-relaxed block mb-4">
                        {entry.description}
                      </Text>
                      <Text className="text-sm text-muted-foreground/60">
                        {format(new Date(entry.date), "MMMM d, yyyy")}
                      </Text>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between mt-8 pt-6 border-t border-neutral-200 dark:border-neutral-800">
        <Button
          variant="tertiary"
          onClick={() => window.open("https://www.speakeasy.com/changelog", "_blank")}
          className="text-base px-4 py-2"
        >
          <Icon name="external-link" className="size-4" />
          View all releases
        </Button>
        <Button onClick={handleClose} variant="brand" className="px-6 py-2 text-base">
          Got it
        </Button>
      </div>
    </div>
  );
}