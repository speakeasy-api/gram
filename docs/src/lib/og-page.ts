import fs from "node:fs/promises";
import { jsx, jsxs } from "react/jsx-runtime";
import satori from "satori";
import sharp from "sharp";

const diatypeRegular = await fs.readFile("./src/fonts/ABCDiatype-Regular.otf");
const diatypeLight = await fs.readFile("./src/fonts/ABCDiatype-Light.otf");
const diatypeMono = await fs.readFile("./src/fonts/ABCDiatypeMono-Regular.otf");
const diatypeMonoLight = await fs.readFile(
  "./src/fonts/ABCDiatypeMono-Light.otf",
);
const tobiasThin = await fs.readFile("./src/fonts/Tobias-Thin.otf");

const speakeasyWordmark = await fs.readFile(
  "./src/assets/speakeasy-wordmark.png",
);
const gramLogo = await fs.readFile("./src/assets/gram-logo.png");

export async function renderOGImage(title: string, description?: string) {
  const svg = await satori(
    jsxs("div", {
      style: {
        display: "flex",
        width: "100%",
        height: "100%",
        background: "white",
      },
      children: [
        jsxs("div", {
          style: {
            width: "100%",
            height: "100%",
            display: "flex",
            alignItems: "flex-start",
            justifyContent: "space-between",
            flexDirection: "column",
            padding: 134,
          },
          children: [
            jsx("img", {
              src: speakeasyWordmark.buffer,
              alt: "",
              width: "160px",
              height: "25px",
            }),
            jsxs("div", {
              style: {
                width: "100%",
                display: "flex",
                alignItems: "flex-end",
                justifyContent: "space-between",
              },
              children: [
                jsxs("div", {
                  style: {
                    display: "flex",
                    flexDirection: "column",
                  },
                  children: [
                    jsx("img", {
                      src: gramLogo.buffer,
                      alt: "Gram",
                      width: "300px",
                      height: "80px",
                      style: {
                        marginBottom: 26,
                        marginLeft: "-4px",
                      },
                    }),
                    jsx("p", {
                      style: {
                        margin: 0,
                        fontFamily: "Diatype, sans-serif",
                        fontWeight: 300,
                        fontSize: "26px",
                        lineHeight: "42px",
                        color: "rgba(17, 17, 17, 0.64)",
                      },
                      children: title,
                    }),
                  ],
                }),
                jsx("p", {
                  style: {
                    flex: "1 1 100%",
                    maxWidth: "330px",
                    margin: 0,
                    fontSize: "18px",
                    lineHeight: "32px",
                  },
                  children: description,
                }),
              ],
            }),
          ],
        }),
        jsx("div", {
          style: {
            fontFamily: "Diatype, sans-serif",
            position: "absolute",
            top: 63,
            left: 0,
            height: "2px",
            width: "100%",
            opacity: 0.2,
            backgroundImage:
              "linear-gradient(to right,rgba(0, 0, 0, 1) 0%, rgba(255, 255, 255, 1) 100%)",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            bottom: 63,
            left: 0,
            height: "2px",
            width: "100%",
            opacity: 0.2,
            backgroundImage:
              "linear-gradient(to left,rgba(0, 0, 0, 1) 0%, rgba(255, 255, 255, 1) 100%)",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            top: 0,
            left: 63,
            height: "100%",
            width: "2px",
            opacity: 0.2,
            backgroundImage:
              "linear-gradient(to bottom,rgba(0, 0, 0, 1) 0%, rgba(255, 255, 255, 1) 100%)",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            top: 0,
            right: 63,
            height: "100%",
            width: "2px",
            opacity: 0.2,
            backgroundImage:
              "linear-gradient(to top,rgba(0, 0, 0, 1) 0%, rgba(255, 255, 255, 1) 100%)",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            top: 64,
            left: 64,
            width: 20,
            height: 20,
            border: "2px solid rgba(0,0,0,0.2)",
            margin: "-10px 0 0 -10px",
            background: "white",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            bottom: 64,
            right: 64,
            width: 20,
            height: 20,
            border: "2px solid rgba(0,0,0,0.2)",
            margin: "0 -10px -10px 0",
            background: "white",
          },
        }),
        jsx("div", {
          style: {
            position: "absolute",
            bottom: 0,
            left: 0,
            width: "100%",
            height: "20px",
            background:
              "linear-gradient(90deg, #320F1E 0%, #C83228 12.56%, #FB873F 25.06%, #D2DC91 37.56%, #5A8250 50.06%, #002314 62.06%, #00143C 74.06%, #2873D7 86.06%, #9BC3FF 97.06%)",
          },
        }),
      ],
    }),
    {
      width: 1200,
      height: 630,
      fonts: [
        {
          name: "Diatype",
          data: diatypeRegular,
          weight: 400,
          style: "normal",
        },
        {
          name: "Diatype",
          data: diatypeLight,
          weight: 300,
          style: "normal",
        },
        {
          name: "Diatype Mono",
          data: diatypeMono,
          weight: 400,
          style: "normal",
        },
        {
          name: "Diatype Mono",
          data: diatypeMonoLight,
          weight: 300,
          style: "normal",
        },
        {
          name: "Tobias",
          data: tobiasThin,
          weight: 700,
          style: "normal",
        },
      ],
    },
  );

  return sharp(Buffer.from(svg)).png().toBuffer();
}
