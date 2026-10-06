import { configure, getConsoleSink, type LogLevel } from "@logtape/logtape";
import { getPrettyFormatter } from "@logtape/pretty";
import { isCI } from "./config.ts";

/**
 * Sends the SDK's log output and the CLI output it relays to the console.
 *
 * @param category The logger category the SDK logs under.
 */
export async function configureLogger(category: string, lowestLevel: LogLevel) {
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
        category: [category],
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
