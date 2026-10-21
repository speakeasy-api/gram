package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CreateSpace creates a named space that permits external participants.
func (c *Client) CreateSpace(ctx context.Context, token, name, requestID string) (Space, error) {
	var space Space
	err := c.Call(ctx, token, http.MethodPost, "https://chat.googleapis.com/v1/spaces?requestId="+requestID, map[string]any{"spaceType": "SPACE", "displayName": name, "externalUserAllowed": true}, &space)
	return space, err
}

// AddApplication ensures the hosted app is a member without treating conflicts as success.
func (c *Client) AddApplication(ctx context.Context, token, space string) error {
	err := c.Call(ctx, token, http.MethodPost, "https://chat.googleapis.com/v1/"+space+"/members", map[string]any{"member": map[string]string{"name": "users/app", "type": "BOT"}}, nil)
	if err == nil {
		return nil
	}
	appToken, e := c.AppToken(ctx, false)
	if e != nil {
		return e
	}
	var verified Space
	return c.Call(ctx, appToken, http.MethodGet, "https://chat.googleapis.com/v1/"+space, nil, &verified)
}

// AddMember invites a human using the connected user's authority.
func (c *Client) AddMember(ctx context.Context, token, space, email string) error {
	err := c.Call(ctx, token, http.MethodPost, "https://chat.googleapis.com/v1/"+space+"/members", map[string]any{"member": map[string]string{"name": "users/" + email, "type": "HUMAN"}}, nil)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusConflict {
		return nil
	}
	return err
}

// GetSpace reads the space under the connected user's authority.
func (c *Client) GetSpace(ctx context.Context, token, name string) (Space, error) {
	var space Space
	err := c.Call(ctx, token, http.MethodGet, "https://chat.googleapis.com/v1/"+name, nil, &space)
	return space, err
}

// CreateSubscription waits for Google's operation to return the created subscription.
func (c *Client) CreateSubscription(ctx context.Context, token, space string) (Subscription, error) {
	var op struct {
		Name     string       `json:"name"`
		Done     bool         `json:"done"`
		Response Subscription `json:"response"`
		Error    *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	err := c.Call(ctx, token, http.MethodPost, "https://workspaceevents.googleapis.com/v1/subscriptions", map[string]any{"targetResource": "//chat.googleapis.com/" + space, "eventTypes": []string{"google.workspace.chat.message.v1.created"}, "notificationEndpoint": map[string]string{"pubsubTopic": c.Config.Topic}, "payloadOptions": map[string]bool{"includeResource": true}, "ttl": "14400s"}, &op)
	if err != nil {
		return Subscription{}, err
	}
	for attempt := 0; op.Response.Name == "" && !op.Done && attempt < 15; attempt++ {
		if !strings.HasPrefix(op.Name, "operations/") || strings.ContainsAny(strings.TrimPrefix(op.Name, "operations/"), "/?#%") {
			return Subscription{}, errors.New("invalid Google operation")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Subscription{}, fmt.Errorf("wait for Google subscription: %w", ctx.Err())
		case <-timer.C:
		}
		if err := c.Call(ctx, token, http.MethodGet, "https://workspaceevents.googleapis.com/v1/"+op.Name, nil, &op); err != nil {
			return Subscription{}, err
		}
	}
	if !op.Done || op.Error != nil || op.Response.Name == "" {
		return Subscription{}, errors.New("google subscription did not complete")
	}
	return op.Response, nil
}

// DeleteSubscription deletes the exact previously stored subscription.
func (c *Client) DeleteSubscription(ctx context.Context, token, name string) error {
	err := c.Call(ctx, token, http.MethodDelete, "https://workspaceevents.googleapis.com/v1/"+name, nil, nil)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// UpsertPushSubscription creates or updates the authenticated push endpoint.
func (c *Client) UpsertPushSubscription(ctx context.Context, name, endpoint, audience, space string) error {
	token, err := c.AppToken(ctx, true)
	if err != nil {
		return err
	}
	config := map[string]any{"pushEndpoint": endpoint, "oidcToken": map[string]string{"serviceAccountEmail": c.Config.PushServiceAccount, "audience": audience}}
	filter := `attributes.ce-subject = "//chat.googleapis.com/` + space + `"`
	body := map[string]any{"topic": c.Config.Topic, "filter": filter, "pushConfig": config}
	endpointURL := "https://pubsub.googleapis.com/v1/" + name
	if err := c.Call(ctx, token, http.MethodPut, endpointURL, body, nil); err == nil {
		return nil
	}
	var existing struct {
		Topic  string `json:"topic"`
		Filter string `json:"filter"`
	}
	if err := c.Call(ctx, token, http.MethodGet, endpointURL, nil, &existing); err != nil {
		return err
	}
	if existing.Topic != c.Config.Topic || existing.Filter != filter {
		if err := c.Call(ctx, token, http.MethodDelete, endpointURL, nil, nil); err != nil {
			return err
		}
		return c.Call(ctx, token, http.MethodPut, endpointURL, body, nil)
	}
	return c.Call(ctx, token, http.MethodPost, endpointURL+":modifyPushConfig", map[string]any{"pushConfig": config}, nil)
}

// NotifyUnmapped sends a retry-safe explanation without running an assistant.
func (c *Client) NotifyUnmapped(ctx context.Context, space, thread, messageID string) error {
	token, err := c.AppToken(ctx, false)
	if err != nil {
		return err
	}
	err = c.Call(ctx, token, http.MethodPost, "https://chat.googleapis.com/v1/"+space+"/messages?messageId="+messageID+"&messageReplyOption=REPLY_MESSAGE_OR_FAIL", map[string]any{"text": "I can't act for you yet — ask an org admin to link your Speakeasy account in Org > Identity > Google Chat.", "thread": map[string]string{"name": thread}}, nil)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusConflict {
		return nil
	}
	return err
}

// GetSubscription reads a stored lease without changing its target.
func (c *Client) GetSubscription(ctx context.Context, token, name string) (Subscription, error) {
	var sub Subscription
	err := c.Call(ctx, token, http.MethodGet, "https://workspaceevents.googleapis.com/v1/"+name, nil, &sub)
	return sub, err
}

// ValidateSubscription checks all delivery properties before activating a lease.
func (c *Client) ValidateSubscription(sub Subscription, space string) error {
	if sub.TargetResource != "//chat.googleapis.com/"+space || sub.State != "ACTIVE" || !sub.PayloadOptions.IncludeResource || len(sub.EventTypes) != 1 || sub.EventTypes[0] != "google.workspace.chat.message.v1.created" || sub.NotificationEndpoint.PubsubTopic != c.Config.Topic {
		return errors.New("google subscription does not match this space's delivery settings")
	}
	return nil
}

// RenewSubscription updates the lease with its current etag, then checks the result.
func (c *Client) RenewSubscription(ctx context.Context, token string, sub Subscription) (Subscription, error) {
	var operation struct {
		Name     string       `json:"name"`
		Done     bool         `json:"done"`
		Response Subscription `json:"response"`
		Error    *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := c.Call(ctx, token, http.MethodPatch, "https://workspaceevents.googleapis.com/v1/"+sub.Name+"?updateMask=ttl", map[string]string{"ttl": "14400s", "etag": sub.Etag}, &operation); err != nil {
		return Subscription{}, fmt.Errorf("extend Google subscription: %w", err)
	}
	for attempt := 0; !operation.Done && operation.Response.Name == "" && attempt < 15; attempt++ {
		if !strings.HasPrefix(operation.Name, "operations/") || strings.ContainsAny(strings.TrimPrefix(operation.Name, "operations/"), "/?#%") {
			return Subscription{}, errors.New("invalid Google renewal operation")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Subscription{}, fmt.Errorf("wait for Google renewal: %w", ctx.Err())
		case <-timer.C:
		}
		if err := c.Call(ctx, token, http.MethodGet, "https://workspaceevents.googleapis.com/v1/"+operation.Name, nil, &operation); err != nil {
			return Subscription{}, fmt.Errorf("read Google renewal: %w", err)
		}
	}
	if operation.Error != nil || (!operation.Done && operation.Response.Name == "") {
		return Subscription{}, errors.New("google subscription renewal did not complete")
	}
	return c.GetSubscription(ctx, token, sub.Name)
}

// DeletePushSubscription removes the managed queue for a disconnected binding.
func (c *Client) DeletePushSubscription(ctx context.Context, id string) error {
	token, err := c.AppToken(ctx, true)
	if err != nil {
		return fmt.Errorf("authorize queue deletion: %w", err)
	}
	name := strings.Split(c.Config.Topic, "/topics/")[0] + "/subscriptions/chat-" + id
	err = c.Call(ctx, token, http.MethodDelete, "https://pubsub.googleapis.com/v1/"+name, nil, nil)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// RestoreSubscription reuses a healthy lease and replaces an expired lease for the same space.
func (c *Client) RestoreSubscription(ctx context.Context, token, name, space string, now time.Time) (Subscription, error) {
	if name == "" {
		return c.CreateSubscription(ctx, token, space)
	}
	sub, err := c.GetSubscription(ctx, token, name)
	var api *APIError
	if errors.As(err, &api) && api.Status == http.StatusNotFound {
		return c.CreateSubscription(ctx, token, space)
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("read existing Google subscription: %w", err)
	}
	if sub.TargetResource != "//chat.googleapis.com/"+space {
		return Subscription{}, errors.New("stored Google subscription belongs to another space")
	}
	if sub.State != "ACTIVE" || !sub.ExpireTime.After(now) {
		if err := c.DeleteSubscription(ctx, token, sub.Name); err != nil {
			return Subscription{}, fmt.Errorf("remove expired Google subscription: %w", err)
		}
		return c.CreateSubscription(ctx, token, space)
	}
	if err := c.ValidateSubscription(sub, space); err != nil {
		return Subscription{}, err
	}
	if !sub.ExpireTime.After(now.Add(2 * time.Hour)) {
		return c.RenewSubscription(ctx, token, sub)
	}
	return sub, nil
}
