type GetPastLaunchesParams = {
  limit?: number;
  sort?: keyof Launch;
  order?: "asc" | "desc";
};

type Launch = {
  flight_number: number;
  mission_name: string;
  launch_date_utc: string;
  launch_site: {
    site_id: string;
    site_name: string;
    site_name_long: string;
  };
};

type Launchpad = {
  id: number;
  name: string;
  location: {
    name: string;
    region: string;
    latitude: number;
    longitude: number;
  };
};

export const getPastLaunches = async (
  params: GetPastLaunchesParams = {},
): Promise<Launch[]> => {
  const url = new URL("https://api.spacexdata.com/v3/launches/past");

  for (const [key, value] of Object.entries(params)) {
    if (!value) continue;
    url.searchParams.append(key, value.toString());
  }

  const result = await fetch(url);
  return result.json() as Promise<Launch[]>;
};

export const getLaunchById = async (id: string): Promise<Launch> => {
  const result = await fetch(
    `https://api.spacexdata.com/v3/launches/${encodeURIComponent(id)}`,
  );
  return result.json() as Promise<Launch>;
};

export const getLaunchpadById = async (siteId: string): Promise<Launchpad> => {
  const result = await fetch(
    `https://api.spacexdata.com/v3/launchpads/${encodeURIComponent(siteId)}`,
  );
  return result.json() as Promise<Launchpad>;
};
