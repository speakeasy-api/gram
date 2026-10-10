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
		TotalCredits *float64 `json:"total_credits"`

		// TotalUsage is all credit ever spent.
		TotalUsage *float64 `json:"total_usage"`
	} `json:"data"`
}

// keyResponse is OpenRouter's GET /v1/key body for the calling key.
type keyResponse struct {
	// Data holds the key's own limit.
	Data struct {
		// IsManagementKey permits account-wide balance lookup.
		IsManagementKey bool `json:"is_management_key"`

		// LimitRemaining is what the key may still spend, or null when the key
		// has no limit of its own.
		LimitRemaining *float64 `json:"limit_remaining"`
	} `json:"data"`
}

// creditBalance describes a checked account balance or key spending allowance.
type creditBalance struct {
	// Remaining is the amount available under the checked limit, in USD.
	Remaining float64

	// AccountVerified distinguishes an account balance from a key-only allowance.
	AccountVerified bool
}

// openRouterCredit checks the current key without requiring a management key.
// An inference key reveals only its own allowance, not the account balance.
func openRouterCredit(ctx context.Context, client *http.Client, baseURL, key string) (creditBalance, error) {
	var keyInfo keyResponse
	if err := getOpenRouterJSON(ctx, client, baseURL+"/v1/key", key, &keyInfo); err != nil {
		return creditBalance{Remaining: 0, AccountVerified: false}, err
	}
	if !keyInfo.Data.IsManagementKey {
		if keyInfo.Data.LimitRemaining == nil {
			return creditBalance{Remaining: 0, AccountVerified: false}, fmt.Errorf("inference key has no reported spending allowance; account balance cannot be verified")
		}
		return creditBalance{Remaining: *keyInfo.Data.LimitRemaining, AccountVerified: false}, nil
	}
	var credits creditsResponse
	if err := getOpenRouterJSON(ctx, client, baseURL+"/v1/credits", key, &credits); err != nil {
		return creditBalance{Remaining: 0, AccountVerified: false}, err
	}
	if credits.Data.TotalCredits == nil || credits.Data.TotalUsage == nil {
		return creditBalance{Remaining: 0, AccountVerified: false}, fmt.Errorf("OpenRouter credit response is missing account totals")
	}
	remaining := *credits.Data.TotalCredits - *credits.Data.TotalUsage
	if limit := keyInfo.Data.LimitRemaining; limit != nil && *limit < remaining {
		remaining = *limit
	}
	return creditBalance{Remaining: remaining, AccountVerified: true}, nil
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

// checkCredit fails when the checked allowance cannot fund a run. A credit check
// that itself fails only warns, so a provider hiccup never blocks a run.
func checkCredit(ctx context.Context, client *http.Client, baseURL, key string, cases int) error {
	need := float64(cases) * costPerCaseUSD
	balance, err := openRouterCredit(ctx, client, baseURL, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not check OpenRouter credit: %v\n", err)
		return nil
	}
	if !balance.AccountVerified {
		fmt.Fprintln(os.Stderr, "warning: checked OpenRouter key allowance only; the account balance may be lower")
	}
	if balance.Remaining < need+creditHeadroomUSD {
		return fmt.Errorf("OpenRouter has $%.2f of checked allowance left and %d cases need about $%.2f, plus $%.2f for calls in flight; check account credit at https://openrouter.ai/settings/credits or the key spending limit and rerun", balance.Remaining, cases, need, creditHeadroomUSD)
	}
	return nil
}
