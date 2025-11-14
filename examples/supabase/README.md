# Gram Functions x Supabase Example

This Gram Function shows how to connect to a Supabase database and running a query to get data from a table that is then returned as a JSON response.

## Usage

- Sign up to [Supabase](https://supabase.com/) and create a new project
- Store your Supabase URL and Anon Key as environment variables:
  - `SUPABASE_URL`
  - `SUPABASE_ANON_KEY`
- Download one of the Yearly CSV files from [Price Paid Data](https://www.gov.uk/government/statistical-data-sets/price-paid-data-downloads) public dataset.
- Connect to your database with:

  ```
  psql -h <db-hostname> -p 5432 -d postgres -U postgres
  ```

  _Replace `<db-hostname>` with your Supabase database hostname, which can be found on the Supabase dashboard_

- Create a table to hold the data:

  ```sql
  CREATE TABLE land_registry_price_paid_uk(
    transaction uuid,
    price numeric,
    transfer_date date,
    postcode text,
    property_type char(1),
    newly_built boolean,
    duration char(1),
    paon text,
    saon text,
    street text,
    locality text,
    city text,
    district text,
    county text,
    ppd_category_type char(1),
    record_status char(1));
  ```

- In the same psql session, load the CSV data into the table:

  ```sql
  \copy land_registry_price_paid_uk FROM '/path/to/pp-complete.csv' with (format csv, encoding 'win1252', header false, null '', quote '"', force_null (postcode, saon, paon, street, locality, city, district))
  ```

- You're all set to build and push this Gram Function!
  - Run `pnpm install && pnpm build && pnpm push`.
