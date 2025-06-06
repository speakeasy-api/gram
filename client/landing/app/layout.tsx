import type { Metadata } from "next";
import localFont from "next/font/local";
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
