import { SimpleTooltip } from "@/components/ui/Tooltip";

export function SlackChannelLink({
  channelId,
  channelName,
  teamId,
}: {
  channelId: string;
  channelName?: string;
  teamId?: string;
}): JSX.Element {
  const label = `#${(channelName || channelId).replace(/^#/, "")}`;
  const content = <span className="max-w-40 truncate">{label}</span>;
  return (
    <SimpleTooltip
      tooltip={
        <div>
          <div>{label}</div>
          <div className="text-xs opacity-75">
            {channelId}
            {teamId ? ` · ${teamId}` : ""}
          </div>
        </div>
      }
    >
      {teamId ? (
        <a
          className="pointer-events-auto inline-flex hover:underline"
          href={`https://app.slack.com/client/${encodeURIComponent(teamId)}/${encodeURIComponent(channelId)}`}
          target="_blank"
          rel="noreferrer"
        >
          {content}
        </a>
      ) : (
        <span className="pointer-events-auto inline-flex">{content}</span>
      )}
    </SimpleTooltip>
  );
}
