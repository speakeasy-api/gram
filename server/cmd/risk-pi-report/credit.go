package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// creditsResponse is OpenRouter's GET /v1/credits body: the account's
// purchased credit and its usage so far, in USD.
type creditsResponse struct {
	// Data holds the account totals.
	Data struct {
		// TotalCredits is all credit ever purchased.
		TotalCredits float64 `json:"total_credits"`

		// TotalUsage is all credit ever spent.
		TotalUsage float64 `json:"total_usage"`
	} `json:"data"`
}

// keyResponse is OpenRouter's GET /v1/key body for the calling key.
type keyResponse struct {
	// Data holds the key's own limit.
	Data struct {
		// LimitRemaining is what the key may still spend, or null when the key
		// has no limit of its own.
		LimitRemaining *float64 `json:"limit_remaining"`
	} `json:"data"`
}

// openRouterCredit returns the credit the key can still spend: the account
// balance, capped by the key's own limit when it has one.
func openRouterCredit(ctx context.Context, client *http.Client, baseURL, key string) (float64, error) {
	var credits creditsResponse
	if err := getOpenRouterJSON(ctx, client, baseURL+"/v1/credits", key, &credits); err != nil {
		return 0, err
	}
	remaining := credits.Data.TotalCredits - credits.Data.TotalUsage
	var keyInfo keyResponse
	if err := getOpenRouterJSON(ctx, client, baseURL+"/v1/key", key, &keyInfo); err != nil {
		return 0, err
	}
	if limit := keyInfo.Data.LimitRemaining; limit != nil && *limit < remaining {
		remaining = *limit
	}
	return remaining, nil
}

func getOpenRouterJSON(ctx context.Context, client *http.Client, url, key string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, creditCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build OpenRouter credit request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request OpenRouter credit: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("request OpenRouter credit: HTTP status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read OpenRouter credit: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode OpenRouter credit: %w", err)
	}
	return nil
}

// checkCredit fails before a run when the key cannot fund it. A credit check
// that itself fails only warns, so a provider hiccup never blocks a run.
func checkCredit(ctx context.Context, client *http.Client, baseURL, key string, cases int) error {
	need := float64(cases) * costPerCaseUSD
	remaining, err := openRouterCredit(ctx, client, baseURL, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not check OpenRouter credit: %v\n", err)
		return nil
	}
	if remaining < need+creditHeadroomUSD {
		return fmt.Errorf("OpenRouter has $%.2f of credit left and %d cases need about $%.2f, plus $%.2f for calls in flight; add credit at https://openrouter.ai/settings/credits and rerun", remaining, cases, need, creditHeadroomUSD)
	}
	return nil
}
