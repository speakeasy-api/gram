import type { Metadata } from "next";
import localFont from "next/font/local";
import Script from "next/script";
import "./globals.css";

const diatype = localFont({
  src: [
    {
      path: "../public/fonts/diatype/ABCDiatype-Light.woff2",
      weight: "300",
      style: "normal",
    },
    {
      path: "../public/fonts/diatype/ABCDiatype-Regular.woff2",
      weight: "400",
      style: "normal",
    },
  ],
  variable: "--font-diatype",
  fallback: ["sans-serif"],
  display: "swap",
});

const diatypeMono = localFont({
  src: [
    {
      path: "../public/fonts/diatype-mono/ABCDiatypeMono-Light.woff2",
      weight: "300",
      style: "normal",
    },
    {
      path: "../public/fonts/diatype-mono/ABCDiatypeMono-Regular.woff2",
      weight: "400",
      style: "normal",
    },
  ],
  variable: "--font-diatype-mono",
  fallback: ["monospace"],
  display: "swap",
});

const tobias = localFont({
  src: [
    {
      path: "../public/fonts/tobias/Tobias-Thin.woff2",
      weight: "100",
      style: "normal",
    },
  ],
  preload: true,
  variable: "--font-tobias",
  fallback: ["serif"],
  display: "swap",
});

// const speakeasyAscii = localFont({
//   src: "../../public/fonts/speakeasy/speakeasy-ascii.woff2",
//   variable: "--font-speakeasy",
//   preload: true,
//   adjustFontFallback: false,
//   display: "swap",
// });

export const metadata: Metadata = {
  title: "Gram by Speakeasy",
  description: "Create, curate and distribute tools for AI",
  openGraph: {
    title: "Gram by Speakeasy",
    description: "Create, curate and distribute tools for AI",
    images: ["https://app.getgram.ai/og-image.png"],
    url: "https://app.getgram.ai",
    type: "website",
    siteName: "Gram",
    locale: "en_US",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <Script
        id="reb2b-tracking"
        strategy="beforeInteractive"
        dangerouslySetInnerHTML={{
          __html: `!function () {var reb2b = window.reb2b = window.reb2b || [];if (reb2b.invoked) return;reb2b.invoked = true;reb2b.methods = ["identify", "collect"];reb2b.factory = function (method) {return function () {var args = Array.prototype.slice.call(arguments);args.unshift(method);reb2b.push(args);return reb2b;};};for (var i = 0; i < reb2b.methods.length; i++) {var key = reb2b.methods[i];reb2b[key] = reb2b.factory(key);}reb2b.load = function (key) {var script = document.createElement("script");script.type = "text/javascript";script.async = true;script.src = "https://ddwl4m2hdecbv.cloudfront.net/b/" + key + "/1N5W0HMGDKO5.js.gz";var first = document.getElementsByTagName("script")[0];first.parentNode.insertBefore(script, first);};reb2b.SNIPPET_VERSION = "1.0.1";reb2b.load("1N5W0HMGDKO5");}();`
        }}
      />
      <body
        className={`
          ${diatype.variable} 
          ${diatypeMono.variable} 
          ${tobias.variable} 
          antialiased
        `}
      >
        {children}
      </body>
    </html>
  );
}
