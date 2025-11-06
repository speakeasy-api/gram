export type OpenMeteoHistoricalResponse = {
  latitude: number;
  longitude: number;
  daily: {
    time: string[];
    temperature_2m_max: number[];
    temperature_2m_min: number[];
    precipitation_sum: number[];
    wind_speed_10m_max: number[];
    wind_direction_10m_dominant: number[];
  };
  daily_units: {
    temperature_2m_max: string;
    precipitation_sum: string;
    wind_speed_10m_max: string;
    wind_direction_10m_dominant: string;
  };
};

export type HistoricalWeather = {
  location: {
    latitude: number;
    longitude: number;
  };
  date: string;
  conditions: {
    maxTempC: number;
    minTempC: number;
    precipitationMm: number;
    maxWindSpeedKmh: number;
    windDirectionDegrees: number;
  };
  units: {
    temperature: string;
    precipitation: string;
    windSpeed: string;
    windDirection: string;
  };
};

/**
 * Fetches historical weather data for a specific location and date using Open-Meteo API.
 *
 * @param lat - Latitude of the location
 * @param lon - Longitude of the location
 * @param date - Date in YYYY-MM-DD format or ISO 8601 timestamp
 * @returns Historical weather data
 */
export const getHistoricalWeather = async (
  lat: number,
  lon: number,
  date: string,
): Promise<HistoricalWeather> => {
  // Convert ISO 8601 timestamp to YYYY-MM-DD if needed
  const dateOnly = date.includes("T") ? date.split("T")[0] : date;

  const url = new URL("https://archive-api.open-meteo.com/v1/archive");
  url.searchParams.append("latitude", lat.toString());
  url.searchParams.append("longitude", lon.toString());
  url.searchParams.append("start_date", dateOnly!);
  url.searchParams.append("end_date", dateOnly!);
  url.searchParams.append(
    "daily",
    "temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max,wind_direction_10m_dominant",
  );

  const result = await fetch(url);
  const weatherData = (await result.json()) as OpenMeteoHistoricalResponse;

  return {
    location: {
      latitude: weatherData.latitude,
      longitude: weatherData.longitude,
    },
    date: weatherData.daily.time[0]!,
    conditions: {
      maxTempC: weatherData.daily.temperature_2m_max[0]!,
      minTempC: weatherData.daily.temperature_2m_min[0]!,
      precipitationMm: weatherData.daily.precipitation_sum[0]!,
      maxWindSpeedKmh: weatherData.daily.wind_speed_10m_max[0]!,
      windDirectionDegrees: weatherData.daily.wind_direction_10m_dominant[0]!,
    },
    units: {
      temperature: weatherData.daily_units.temperature_2m_max,
      precipitation: weatherData.daily_units.precipitation_sum,
      windSpeed: weatherData.daily_units.wind_speed_10m_max,
      windDirection: weatherData.daily_units.wind_direction_10m_dominant,
    },
  };
};
