"use client";

import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { PortalContainerProvider } from "#elements/contexts/portal-container";
import { useElements } from "#elements/hooks/useElements";
import { useThemeProps } from "#elements/hooks/useThemeProps";
import { cn } from "#elements/lib/utils";
import { ROOT_SELECTOR } from "#elements/constants/tailwind";
import elementsStyles from "#elements/global.css?inline";

interface ShadowRootProps {
  children: ReactNode;
  hostClassName?: string;
  hostStyle?: CSSProperties;
}

export const ShadowRoot = ({
  children,
  hostClassName,
  hostStyle,
}: ShadowRootProps): React.JSX.Element => {
  const hostRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [shadowRoot, setShadowRoot] = useState<ShadowRoot | null>(null);
  const { config } = useElements();
  const themeProps = useThemeProps();

  const rootClassName = useMemo(
    () => cn(ROOT_SELECTOR, themeProps.className),
    [themeProps.className],
  );

  useEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return;
    }

    const root = host.shadowRoot ?? host.attachShadow({ mode: "open" });
    setShadowRoot(root);
  }, []);

  useEffect(() => {
    if (!shadowRoot) {
      return;
    }

    const existingStyle = shadowRoot.querySelector<HTMLStyleElement>(
      "style[data-gram-elements]",
    );

    if (existingStyle) {
      existingStyle.textContent = elementsStyles;
      return;
    }

    const styleElement = document.createElement("style");
    styleElement.setAttribute("data-gram-elements", "true");
    styleElement.textContent = elementsStyles;
    shadowRoot.prepend(styleElement);
  }, [shadowRoot]);

  // Embedder CSS overrides (theme.customCss) go in a separate style tag
  // appended after the built-in stylesheet so they win the cascade at equal
  // specificity.
  const customCss = config.theme?.customCss;
  useEffect(() => {
    if (!shadowRoot) {
      return;
    }

    let customStyle = shadowRoot.querySelector<HTMLStyleElement>(
      "style[data-gram-elements-custom]",
    );

    if (!customCss) {
      customStyle?.remove();
      return;
    }

    if (!customStyle) {
      customStyle = document.createElement("style");
      customStyle.setAttribute("data-gram-elements-custom", "true");
      shadowRoot.append(customStyle);
    }
    customStyle.textContent = customCss;
  }, [shadowRoot, customCss]);

  return (
    <div
      ref={hostRef}
      className={hostClassName}
      style={{ isolation: "isolate", ...hostStyle }}
    >
      {shadowRoot
        ? createPortal(
            <div
              ref={containerRef}
              className={rootClassName}
              data-radius={config.theme?.radius}
              style={
                config.variant === "standalone"
                  ? { height: "100%", width: "100%" }
                  : undefined
              }
            >
              <PortalContainerProvider containerRef={containerRef}>
                {children}
              </PortalContainerProvider>
            </div>,
            shadowRoot,
          )
        : null}
    </div>
  );
};
