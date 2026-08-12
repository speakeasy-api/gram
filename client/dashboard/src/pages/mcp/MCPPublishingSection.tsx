import { Block, BlockInner } from "@/components/block";
import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import {
  usePublishing,
  type PublishingTarget,
} from "@/pages/mcp/usePublishing";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { PageSection } from "./MCPDetails";

export function MCPPublishingSection({
  target,
  canPublish,
  disabledMessage,
}: {
  target: PublishingTarget;
  canPublish: boolean;
  disabledMessage: string;
}): React.JSX.Element {
  const {
    collections,
    effectiveSelected,
    hasChanges,
    isSaving,
    isLoading,
    toggleCollection,
    handleSave,
    handleDiscard,
  } = usePublishing(target);

  return (
    // Publishing attaches the server to an org-level collection, which the
    // collections service authorizes as org:admin (see AttachServer /
    // DetachServer). Gate to match: non-admins see the section greyed out with
    // a permission tooltip rather than controls that would 403. className keeps
    // the block-level PageSection full-width inside the disabled wrapper.
    <RequireScope
      scope="org:admin"
      level="component"
      className="w-full"
      reason="Only organization admins can publish servers to collections."
    >
      <PageSection
        heading="Publishing"
        description="Publish this server to collections so it can be discovered and installed by others in your organization."
      >
        <Block label="Collections" className="p-0">
          <BlockInner>
            {!canPublish ? (
              <Text muted small>
                {disabledMessage}
              </Text>
            ) : isLoading ? (
              <Text muted small>
                Loading collections...
              </Text>
            ) : collections.length === 0 ? (
              <Text muted small>
                No collections available.
              </Text>
            ) : (
              <Stack direction="vertical" gap={2}>
                {collections.map((collection) => (
                  <label
                    key={collection.id}
                    className="flex cursor-pointer items-center gap-3"
                  >
                    <Checkbox
                      checked={effectiveSelected.has(collection.id)}
                      disabled={isSaving}
                      onCheckedChange={() => toggleCollection(collection.id)}
                    />
                    <Stack direction="vertical" gap={0}>
                      <Text small className="font-medium">
                        {collection.name}
                      </Text>
                      {collection.description && (
                        <Text muted small>
                          {collection.description}
                        </Text>
                      )}
                    </Stack>
                  </label>
                ))}
              </Stack>
            )}
          </BlockInner>
          {hasChanges && (
            <BlockInner>
              <Stack direction="horizontal" gap={2}>
                <Button
                  size="sm"
                  disabled={isSaving}
                  onClick={() => void handleSave()}
                >
                  <Button.Text>{isSaving ? "Saving..." : "Save"}</Button.Text>
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={isSaving}
                  onClick={handleDiscard}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
              </Stack>
            </BlockInner>
          )}
        </Block>
      </PageSection>
    </RequireScope>
  );
}
