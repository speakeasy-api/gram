// Gutternote feedback widget loader, for PR preview environments.
//
// This lives in the main bundle rather than an inline <script> in index.html:
// the app is served with `script-src 'self'` and no 'unsafe-inline', so an
// inline script would be blocked in every deployed environment. (Unlike
// theme-init.ts it does not need to run before first paint, so it needs no
// standalone classic-script chunk either — a side-effect import is enough.)
//
// Configuration arrives as meta tags that the dashboard image's entrypoint
// (41-preview-runtime-values.sh) substitutes from the environment at container
// start. Under `vite dev` the entrypoint never runs, so the tags hold literal
// "${...}" text and both checks below fail — the widget stays off locally.

const WIDGET_SRC = "https://cdn.gutternote.com/v1/widget.js";

function metaContent(name: string): string {
  return (
    document
      .querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
      ?.content.trim() ?? ""
  );
}

export function initGutternote(): void {
  // Both gates fail closed on the image's empty ENV defaults: prod, shared
  // staging and local dev never load the widget, and a preview missing the
  // key loads nothing rather than a widget that cannot authenticate.
  const key = metaContent("gutternote-key");
  if (metaContent("gram-is-preview") !== "true" || !key) {
    return;
  }

  if (document.querySelector(`script[src="${WIDGET_SRC}"]`)) {
    return;
  }

  const script = document.createElement("script");
  script.src = WIDGET_SRC;
  script.defer = true;
  script.dataset["gutternoteKey"] = key;
  document.head.appendChild(script);
}
