package okta

import "time"

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

type appJSON struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Name        string    `json:"name"`
	SignOnMode  string    `json:"signOnMode"`
	Status      string    `json:"status"`
	Features    []string  `json:"features"`
	Created     time.Time `json:"created"`
	LastUpdated time.Time `json:"lastUpdated"`
}

func (a appJSON) toApp() App {
	features := a.Features
	if features == nil {
		features = []string{}
	}
	return App{
		ID:          a.ID,
		Label:       a.Label,
		Name:        a.Name,
		SignOnMode:  a.SignOnMode,
		Status:      a.Status,
		Features:    features,
		Created:     a.Created,
		LastUpdated: a.LastUpdated,
	}
}

type appUserJSON struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"`
	Status      string    `json:"status"`
	Created     time.Time `json:"created"`
	LastUpdated time.Time `json:"lastUpdated"`
	Credentials struct {
		UserName string `json:"userName"`
	} `json:"credentials"`
}

type appGroupJSON struct {
	ID          string    `json:"id"`
	Priority    int       `json:"priority"`
	LastUpdated time.Time `json:"lastUpdated"`
}

type groupJSON struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Created     time.Time `json:"created"`
	LastUpdated time.Time `json:"lastUpdated"`
	Profile     struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"profile"`
}
