import { configure, getConsoleSink, type LogLevel } from "@logtape/logtape";
import { getPrettyFormatter } from "@logtape/pretty";
import pkg from "../../package.json" with { type: "json" };
import { isCI } from "./config.ts";

/** Sends the SDK's log output and the CLI output it relays to the console. */
export async function configureLogger(lowestLevel: LogLevel) {
  await configure({
    sinks: {
      console: getConsoleSink({
        formatter: getPrettyFormatter({
          colors: !isCI,
          icons: !isCI,
          properties: true,
          timestampStyle: null,
          levelStyle: ["bold"],
          categoryStyle: ["italic"],
          messageStyle: null,
        }),
      }),
    },

    loggers: [
      {
        category: ["logtape", "meta"],
        lowestLevel: "warning",
        sinks: ["console"],
      },
      {
        category: [pkg.name],
        lowestLevel,
        sinks: ["console"],
      },
      {
        category: ["gram", "cli"],
        lowestLevel,
        sinks: ["console"],
      },
    ],
  });
}
