import { RefObject, useEffect } from "react";

/** Marks the Elements shadow host while a composer menu (slash commands, tool
 *  mentions) is open.
 *
 *  Those menus are absolutely positioned and open past the composer's own box.
 *  A host that clips the composer — the docked pill animates it inside an
 *  `overflow: hidden` grid-rows tray — crops them. The host page can relax that
 *  clip with `:has([data-composer-menu-open])`, but only because the shadow
 *  HOST is in its light tree: no selector can reach the menu itself.
 */
export function useComposerMenuOpen(
  open: boolean,
  ref: RefObject<HTMLElement | null>,
): void {
  useEffect(() => {
    const root = ref.current?.getRootNode();
    const host = root instanceof ShadowRoot ? root.host : null;
    if (!(host instanceof HTMLElement)) return;

    host.toggleAttribute("data-composer-menu-open", open);
    return () => host.removeAttribute("data-composer-menu-open");
  }, [open, ref]);
}
