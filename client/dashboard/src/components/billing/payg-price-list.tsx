import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useTrialNow } from "@/hooks/useTrialNow";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import { useGetUsageTiers } from "@gram/client/react-query/getUsageTiers.js";

/** The server-owned PAYG price list shown before an admin adds a card. */
export function PaygPriceList(): JSX.Element | null {
  const { trial } = useSession();
  const now = useTrialNow(trial);

  if (getTrialLifecycleFromDates(trial, now) !== "active") return null;

  return <PaygPriceListBody />;
}

function PaygPriceListBody(): JSX.Element | null {
  const { data, isLoading, isError } = useGetUsageTiers({
    throwOnError: false,
  });

  if (isError) return null;

  return (
    <Page.Section>
      <Page.Section.Title area="">Pay as you go pricing</Page.Section.Title>
      <Page.Section.Description>
        What this organization pays once the trial converts.
      </Page.Section.Description>
      <Page.Section.Body>
        {isLoading || !data ? (
          <Skeleton className="h-32 w-full" />
        ) : (
          <Stack gap={4}>
            {data.payg.tumPricePerMillionUsd ? (
              <div>
                <Text className="font-medium">Tokens under management</Text>
                <Text muted>
                  ${data.payg.tumPricePerMillionUsd} per million tokens
                </Text>
              </div>
            ) : null}
            <ul className="space-y-2">
              {data.payg.includedBullets?.map((item) => (
                <li key={item}>
                  <span className="text-muted-foreground/60" aria-hidden="true">
                    ✓
                  </span>{" "}
                  {item}
                </li>
              ))}
            </ul>
            <div>
              <Text className="font-medium">Enterprise feature set</Text>
              <ul className="mt-2 grid gap-2 sm:grid-cols-2">
                {data.payg.featureBullets.map((feature) => (
                  <li key={feature}>
                    <span
                      className="text-muted-foreground/60"
                      aria-hidden="true"
                    >
                      ✓
                    </span>{" "}
                    {feature}
                  </li>
                ))}
              </ul>
            </div>
          </Stack>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
