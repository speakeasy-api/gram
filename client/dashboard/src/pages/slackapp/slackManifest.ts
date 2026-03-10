import { SlackAppResult } from "@gram/client/models/components/slackappresult.js";
import { getServerURL } from "@/lib/utils";

export const BOT_SCOPES = [
  "app_mentions:read",
  "chat:write",
  "im:history",
  "im:read",
  "im:write",
  "users:read",
  "channels:history",
  "channels:read",
  "reactions:write",
];

export const BOT_EVENTS = ["app_mention", "message.im"];

function assetImageUrl(assetId: string): string {
  return `${getServerURL()}/rpc/assets.serveImage?id=${assetId}`;
}

export function buildSlackManifest(app: SlackAppResult): object {
  return {
    display_information: {
      name: app.name,
      ...(app.iconAssetId && { icon_url: assetImageUrl(app.iconAssetId) }),
    },
    features: {
      bot_user: { display_name: app.name, always_online: true },
    },
    oauth_config: {
      redirect_urls: app.redirectUrl ? [app.redirectUrl] : [],
      scopes: { bot: BOT_SCOPES },
    },
    settings: {
      event_subscriptions: {
        request_url: app.requestUrl ?? "",
        bot_events: BOT_EVENTS,
      },
    },
  };
}

export function buildDeepLinkUrl(app: SlackAppResult): string {
  const manifest = JSON.stringify(buildSlackManifest(app));
  return `https://api.slack.com/apps?new_app=1&manifest_json=${encodeURIComponent(manifest)}`;
}

export function buildInviteUrl(
  app: SlackAppResult,
  clientId: string,
  returnUrl?: string,
): string {
  const scopes = BOT_SCOPES.join(",");
  const params = new URLSearchParams({
    client_id: clientId,
    scope: scopes,
    redirect_uri: app.redirectUrl ?? "",
  });
  if (returnUrl) {
    params.set("state", returnUrl);
  }
  return `https://slack.com/oauth/v2/authorize?${params.toString()}`;
}
