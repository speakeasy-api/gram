# Gram CLI

## Local Development

1. Setup environment
   - `export GRAM_API_URL=https://localhost:8080`
   - `export GRAM_DASHBOARD_URL=https://localhost:5173`
   - `export GRAM_ORG=organization-123`
   - `export GRAM_PROJECT=default`
   - `export GRAM_API_KEY=<API-KEY>`

2. Run desired command
   - `cd cli`
   - `go run main.go status`

### Testing Gram Functions

1. Stage zip
   - `go run main.go stage function --slug test-fn --location fixtures/example.zip`
   - _You never need to do this again as long as you are using the same zip_

2. Push
   - `go run main.go push`
