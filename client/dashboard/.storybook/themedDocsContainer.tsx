import { DocsContainer } from "@storybook/addon-docs/blocks";
import React, { useEffect, useState } from "react";
import { themes } from "storybook/theming";
import { gramTheme } from "./manager";

import { addons } from "storybook/preview-api";

const DARK_MODE_EVENT_NAME = "DARK_MODE";

function useDocumentClassListObserver(
  callback: (classList: DOMTokenList) => void,
): void {
  useEffect(() => {
    const observer = new MutationObserver((mutationsList) => {
      for (const mutation of mutationsList) {
        if (mutation.attributeName === "class") {
          callback(document.documentElement.classList);
        }
      }
    });

    observer.observe(document.documentElement, { attributes: true });

    return () => observer.disconnect();
  }, [callback]);
}

export const ThemedDocsContainer = ({
  children,
  context,
}: React.ComponentProps<typeof DocsContainer>): React.JSX.Element => {
  const [isDark, setIsDark] = useState(
    document.documentElement.classList.contains("dark"),
  );

  useDocumentClassListObserver((classList) => {
    setIsDark(classList.contains("dark"));
  });
  useEffect(() => {
    const chan = addons.getChannel();
    chan.on(DARK_MODE_EVENT_NAME, setIsDark);
    return () => chan.off(DARK_MODE_EVENT_NAME, setIsDark);
  }, []);

  return (
    <DocsContainer context={context} theme={isDark ? gramTheme : themes.light}>
      {children}
    </DocsContainer>
  );
};
