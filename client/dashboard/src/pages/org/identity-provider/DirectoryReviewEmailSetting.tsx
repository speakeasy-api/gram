import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import {
  invalidateAllDirectoryReviewSettings,
  useDirectoryReviewSettings,
} from "@gram/client/react-query/directoryReviewSettings.js";
import { useUpdateDirectoryReviewSettingsMutation } from "@gram/client/react-query/updateDirectoryReviewSettings.js";

/**
 * Whether the organization's admins are emailed when directory changes are
 * waiting for a decision. Shown whether or not anything is pending, so admins
 * can turn the emails off before the first one arrives.
 */
export function DirectoryReviewEmailSetting(): JSX.Element | null {
  const queryClient = useQueryClient();
  const { data } = useDirectoryReviewSettings(undefined, undefined, {
    throwOnError: false,
  });
  const update = useUpdateDirectoryReviewSettingsMutation({
    retry: false,
    onSuccess: (settings) => {
      void invalidateAllDirectoryReviewSettings(queryClient);
      toast.success(
        settings.emailsEnabled
          ? "Admins will be emailed about directory changes to review."
          : "Admins will no longer be emailed about directory changes to review.",
      );
    },
    onError: () => {
      toast.error("Couldn't update the email setting. Try again.");
    },
  });

  if (!data) return null;

  return (
    <div className="flex items-center justify-between gap-3">
      <div>
        <Text small className="font-medium">
          Email admins about directory changes to review
        </Text>
        <Text muted small>
          One email per batch of changes, with a weekly reminder while they
          wait. Changes are listed here either way.
        </Text>
      </div>
      <RequireScope scope="org:admin" level="component">
        <Switch
          checked={data.emailsEnabled}
          disabled={update.isPending}
          onCheckedChange={(emailsEnabled) =>
            update.mutate({
              request: { directoryReviewSettings: { emailsEnabled } },
            })
          }
          aria-label="Email admins about directory changes to review"
        />
      </RequireScope>
    </div>
  );
}
