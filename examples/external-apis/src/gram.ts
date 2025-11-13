import { Gram } from "@gram-ai/functions";
import { z } from "zod";

/**
 * This example shows how to use the Gram Functions framework to create a tool that calls an external API.
 * It uses the OpenWeatherMap API to get the current weather conditions for a specific city.
 *
 * After running `pnpm create @gram-ai/function@latest`, replace the contents of `src/gram.ts` with this example.
 */

const gram = new Gram({
  envSchema: {
    OPENWEATHER_API_KEY: z.string(),
  },
})
  .tool({
    name: "get_current_weather",
    description: "Get current weather conditions for a specific city",
    inputSchema: {
      city: z.string().describe("The city name (e.g., 'London', 'New York')"),
      country: z
        .string()
        .optional()
        .describe("Optional 2-letter country code (e.g., 'US', 'GB')"),
    },
    async execute(ctx, input) {
      const query =
        input.country != null ? `${input.city},${input.country}` : input.city;

      const url = new URL("https://api.openweathermap.org/data/2.5/weather");
      url.searchParams.append("q", query);
      url.searchParams.append("appid", ctx.env.OPENWEATHER_API_KEY);

      // Gram Functions handle Response objects natively, so no need to process the response at all
      return await fetch(url.toString());
    },
  })
  .tool({
    name: "compare_weather_between_cities",
    description:
      "Compare weather conditions between multiple cities and provide analysis",
    inputSchema: {
      cities: z
        .array(z.string())
        .min(2)
        .max(5)
        .describe(
          "Array of city names to compare (between 2 and 5 cities, e.g., ['London', 'Paris', 'Berlin'])",
        ),
      units: z
        .enum(["metric", "imperial", "standard"])
        .default("metric")
        .describe("Units of measurement for all cities"),
    },
    async execute(ctx, input) {
      // Fetch weather for all cities in parallel
      const weatherPromises = input.cities.map(async (city) => {
        const url = new URL("https://api.openweathermap.org/data/2.5/weather");
        url.searchParams.append("q", city);
        url.searchParams.append("appid", ctx.env.OPENWEATHER_API_KEY);
        url.searchParams.append("units", input.units);

        try {
          const response = await fetch(url.toString());
          if (!response.ok) {
            return { city, error: "City not found or API error" };
          }
          const data: any = await response.json();
          return {
            city: data.name,
            country: data.sys.country,
            temperature: data.main.temp,
            feels_like: data.main.feels_like,
            humidity: data.main.humidity,
            description: data.weather[0].description,
            wind_speed: data.wind.speed,
          };
        } catch (error) {
          return { city, error: "Failed to fetch weather" };
        }
      });

      const results = await Promise.all(weatherPromises);

      // Filter out errors
      const validResults = results.filter((r) => !("error" in r));
      const errors = results.filter((r) => "error" in r);

      if (validResults.length === 0) {
        return ctx.json({
          error: "Could not fetch weather for any cities",
          errors,
        });
      }

      // Calculate comparison statistics
      const temperatures = validResults.map((r) => r.temperature);
      const warmest = validResults.reduce((prev, current) =>
        prev.temperature > current.temperature ? prev : current,
      );
      const coldest = validResults.reduce((prev, current) =>
        prev.temperature < current.temperature ? prev : current,
      );
      const avgTemp =
        temperatures.reduce((sum, temp) => sum + temp, 0) / temperatures.length;

      // Find cities with similar conditions
      const conditionGroups = validResults.reduce(
        (groups, result) => {
          const desc = result.description;
          if (!groups[desc]) groups[desc] = [];
          groups[desc].push(result.city);
          return groups;
        },
        {} as Record<string, string[]>,
      );

      return ctx.json({
        comparison: validResults,
        analysis: {
          warmest_city: {
            city: warmest.city,
            temperature: warmest.temperature,
          },
          coldest_city: {
            city: coldest.city,
            temperature: coldest.temperature,
          },
          temperature_range: warmest.temperature - coldest.temperature,
          average_temperature: Math.round(avgTemp * 10) / 10,
          condition_groups: conditionGroups,
        },
        errors: errors.length > 0 ? errors : undefined,
        units: input.units,
      });
    },
  });

export default gram;
