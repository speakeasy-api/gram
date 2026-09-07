/**
 * Marks the shadow host that `ChatComposer` renders, so hosts can reach the
 * input inside it.
 *
 * Lives here rather than beside the component because a module that exports
 * both components and helpers breaks React Fast Refresh.
 */
export const COMPOSER_HOST_CLASS = "gram-elements-composer-host";

/**
 * Focus the composer's input from outside the shadow root — e.g. the dock's
 * Cmd+/ shortcut. `root` is any ancestor element of the ChatComposer; shadow
 * DOM is queried explicitly because `querySelector` does not cross it.
 */
export function focusChatComposer(root: HTMLElement | null): void {
  root
    ?.querySelector<HTMLElement>(`.${COMPOSER_HOST_CLASS}`)
    ?.shadowRoot?.querySelector<HTMLElement>(".aui-composer-input")
    ?.focus();
}
