import { Gram } from "@gram-ai/functions";
import { z } from "zod/mini";
import * as spacex from "./spacex.ts";
import * as weather from "./weather.ts";

const gram = new Gram()
  .tool({
    name: "get_last_n_launches",
    description: 'Get the details of the last "n" launches from SpaceX.',
    inputSchema: {
      n: z.optional(z.number().check(z.minimum(1))),
    },
    async execute(ctx, input) {
      try {
        const launches = await spacex.getPastLaunches({
          limit: input.n ?? 5,
        });
        return ctx.json(launches);
      } catch (error) {
        return ctx.fail({
          error: error instanceof Error ? error.message : "An error occurred",
        });
      }
    },
  })
  .tool({
    name: "get_launch_weather",
    description:
      "Get historical weather data for a specific SpaceX launch by flight number",
    inputSchema: {
      flightNumber: z.number(),
    },
    async execute(ctx, input) {
      try {
        // Fetch launch details
        const launch = await spacex.getLaunchById(
          input.flightNumber.toString(),
        );

        // Fetch launchpad details to get coordinates
        const launchpad = await spacex.getLaunchpadById(
          launch.launch_site.site_id,
        );

        // Fetch historical weather for the launch
        const historicalWeather = await weather.getHistoricalWeather(
          launchpad.location.latitude,
          launchpad.location.longitude,
          launch.launch_date_utc,
        );

        return ctx.json({
          launch: {
            flightNumber: launch.flight_number,
            missionName: launch.mission_name,
            launchDate: launch.launch_date_utc,
            launchSite: {
              id: launch.launch_site.site_id,
              name: launch.launch_site.site_name,
              nameLong: launch.launch_site.site_name_long,
            },
          },
          launchpad: {
            id: launchpad.id,
            name: launchpad.name,
            location: launchpad.location,
          },
          weather: historicalWeather,
        });
      } catch (error) {
        return ctx.fail({
          error: error instanceof Error ? error.message : "An error occurred",
        });
      }
    },
  });

export default gram;
