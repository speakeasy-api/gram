import { GramLogo } from "@/components/gram-logo";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { fetchChangelog, type ChangelogEntry } from "@/services/changelog";
import { Badge, Heading, Icon, Skeleton, Text } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";

export function ChangelogModal({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [entry, setEntry] = useState<ChangelogEntry | null>(null);
  const [changelogUrl, setChangelogUrl] = useState<string>("");

  useEffect(() => {
    if (open) {
      loadChangelog();
    }
  }, [open]);

  const loadChangelog = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetchChangelog();
      setEntry(response.latestVersion);
      setChangelogUrl(response.changelogUrl);
    } catch (err) {
      console.error("Error fetching changelog:", err);
      setError("Unable to load changelog at this time");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-4xl max-h-[80vh] flex flex-col">
        <Dialog.Header>
          <Dialog.Title className="mb-6">
            <div className="flex items-center gap-4">
              <div className="p-3 rounded-xl bg-linear-to-br from-brand-500/10 to-brand-600/10 border border-brand-500/20">
                <GramLogo variant="icon" className="size-8" />
              </div>
              <div className="flex-1">
                <Heading className="flex items-center gap-3 text-xl">
                  What's new in Gram
                  {entry && (
                    <Badge variant="success" className="text-sm px-3 py-1">
                      {entry.version}
                    </Badge>
                  )}
                </Heading>
                <Text className="text-muted-foreground text-base mt-2">
                  Latest updates and features for the Gram platform
                </Text>
              </div>
            </div>
          </Dialog.Title>
        </Dialog.Header>

        {/* Content */}
        <div className="flex-1 overflow-y-auto min-h-0 -mx-2 px-2">
          {loading ? (
            <div className="space-y-4 w-full">
              <Skeleton className="h-8 w-64 mb-4">
                <div className="h-8 w-64" />
              </Skeleton>
              <Skeleton className="h-4 w-full mb-2">
                <div className="h-4 w-full" />
              </Skeleton>
              <Skeleton className="h-4 w-5/6">
                <div className="h-4 w-5/6" />
              </Skeleton>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center py-12">
              <Icon
                name="circle-alert"
                className="size-12 text-muted-foreground mb-3"
              />
              <Text className="text-muted-foreground">{error}</Text>
              <Button
                variant="outline"
                onClick={loadChangelog}
                className="mt-3"
              >
                <Icon name="refresh-cw" className="size-4" />
                Try again
              </Button>
            </div>
          ) : entry ? (
            <div className="space-y-6 w-full">
              {/* Version Title and Description */}
              <div>
                <div className="flex items-center gap-3 mb-3">
                  <Heading className="text-2xl font-semibold">
                    {entry.title}
                  </Heading>
                </div>
                <Text className="text-base text-muted-foreground leading-relaxed">
                  {entry.description}
                </Text>
              </div>

              {/* Features */}
              {entry.features.length > 0 && (
                <div>
                  <div className="flex items-center gap-2 mb-3">
                    <Icon name="sparkles" className="size-5 text-brand-500" />
                    <Heading className="text-lg font-semibold">
                      Features
                    </Heading>
                  </div>
                  <ul className="space-y-2">
                    {entry.features.map((feature, idx) => (
                      <li key={idx} className="flex items-start gap-3 group">
                        <span className="text-muted-foreground mt-1.5">•</span>
                        <div className="flex-1">
                          <Text className="text-base">
                            {feature.description}
                            {feature.prNumber && (
                              <a
                                href={`https://github.com/speakeasy-api/gram/pull/${feature.prNumber}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="ml-2 text-sm text-brand-500 hover:text-brand-600 hover:underline"
                              >
                                #{feature.prNumber}
                              </a>
                            )}
                          </Text>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Bug Fixes */}
              {entry.bugFixes.length > 0 && (
                <div>
                  <div className="flex items-center gap-2 mb-3">
                    <Icon name="wrench" className="size-5 text-warning" />
                    <Heading className="text-lg font-semibold">
                      Bug Fixes
                    </Heading>
                  </div>
                  <ul className="space-y-2">
                    {entry.bugFixes.map((fix, idx) => (
                      <li key={idx} className="flex items-start gap-3 group">
                        <span className="text-muted-foreground mt-1.5">•</span>
                        <div className="flex-1">
                          <Text className="text-base">
                            {fix.description}
                            {fix.prNumber && (
                              <a
                                href={`https://github.com/speakeasy-api/gram/pull/${fix.prNumber}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="ml-2 text-sm text-brand-500 hover:text-brand-600 hover:underline"
                              >
                                #{fix.prNumber}
                              </a>
                            )}
                          </Text>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          ) : null}
        </div>

        {/* Footer */}
        <Dialog.Footer className="mt-6 pt-6 justify-between">
          <Button
            variant="ghost"
            onClick={() =>
              window.open(
                changelogUrl || "https://www.speakeasy.com/changelog/gram",
                "_blank",
              )
            }
            className="text-base px-4 py-2"
          >
            <Icon name="external-link" className="size-4" />
            View all releases
          </Button>
          <Button
            onClick={() => onOpenChange(false)}
            className="px-6 py-2 text-base"
          >
            Got it
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
