package loopsync

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
)

type Reconciler struct {
	API API
	Log io.Writer
}

// ResolveExisting maps manifest keys to existing Loops transactional email IDs without modifying templates.
func ResolveExisting(ctx context.Context, api API, manifest *Manifest) (map[string]string, error) {
	all, err := api.ListTransactionalEmails(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Loops transactional emails: %w", err)
	}

	byName := make(map[string][]TransactionalEmail, len(all))
	for _, transactional := range all {
		byName[transactional.Name] = append(byName[transactional.Name], transactional)
	}

	resolved := make(map[string]string, len(manifest.Templates))
	for key, spec := range manifest.Templates {
		matches := byName[spec.ManagedName]
		if len(matches) != 1 {
			return nil, fmt.Errorf("resolve email template %q: found %d Loops emails named %q", key, len(matches), spec.ManagedName)
		}
		resolved[key] = matches[0].ID
	}
	return resolved, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, manifest *Manifest, existingIDs map[string]string) (map[string]string, error) {
	if r.API == nil {
		return nil, fmt.Errorf("reconcile email templates: API is required")
	}
	all, err := r.API.ListTransactionalEmails(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Loops transactional emails: %w", err)
	}
	byName := make(map[string][]TransactionalEmail, len(all))
	for _, transactional := range all {
		byName[transactional.Name] = append(byName[transactional.Name], transactional)
	}

	keys := make([]string, 0, len(manifest.Templates))
	for key := range manifest.Templates {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	resolved := make(map[string]string, len(keys))
	for _, key := range keys {
		spec := manifest.Templates[key]
		transactional, err := r.resolve(ctx, key, spec, existingIDs[key], byName)
		if err != nil {
			return nil, err
		}
		resolved[key] = transactional.ID

		if err := r.syncOne(ctx, manifest.Defaults, spec, transactional); err != nil {
			return nil, fmt.Errorf("sync email template %q: %w", key, err)
		}
		if r.Log != nil {
			_, _ = fmt.Fprintf(r.Log, "%s: published\n", key)
		}
	}
	return resolved, nil
}

func (r *Reconciler) resolve(ctx context.Context, key string, spec TemplateSpec, mappedID string, byName map[string][]TransactionalEmail) (TransactionalEmail, error) {
	if mappedID != "" {
		transactional, err := r.API.GetTransactionalEmail(ctx, mappedID)
		if err != nil {
			return TransactionalEmail{}, fmt.Errorf("resolve mapped email template %q: %w", key, err)
		}
		if transactional.Name != spec.ManagedName {
			return TransactionalEmail{}, fmt.Errorf("resolve mapped email template %q: ID %q belongs to %q, want %q", key, mappedID, transactional.Name, spec.ManagedName)
		}
		return transactional, nil
	}

	matches := byName[spec.ManagedName]
	if len(matches) > 1 {
		return TransactionalEmail{}, fmt.Errorf("resolve email template %q: found %d Loops emails named %q", key, len(matches), spec.ManagedName)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	transactional, err := r.API.CreateTransactionalEmail(ctx, spec.ManagedName)
	if err != nil {
		return TransactionalEmail{}, fmt.Errorf("create email template %q: %w", key, err)
	}
	return transactional, nil
}

func (r *Reconciler) syncOne(ctx context.Context, defaults MessageDefaults, spec TemplateSpec, transactional TransactionalEmail) error {
	transactional, err := r.API.EnsureDraft(ctx, transactional.ID)
	if err != nil {
		return fmt.Errorf("ensure draft: %w", err)
	}
	if transactional.DraftEmailMessageID == nil {
		return fmt.Errorf("ensure draft: Loops returned no draft email message ID")
	}

	draft, err := r.API.GetEmailMessage(ctx, *transactional.DraftEmailMessageID)
	if err != nil {
		return fmt.Errorf("get draft message: %w", err)
	}
	draft, err = r.API.UpdateEmailMessage(ctx, draft.ID, UpdateEmailMessageInput{
		ExpectedRevisionID: draft.ContentRevisionID,
		Subject:            spec.Subject,
		PreviewText:        spec.PreviewText,
		FromName:           defaults.FromName,
		FromEmail:          defaults.FromEmail,
		ReplyToEmail:       defaults.ReplyToEmail,
		EmailFormat:        "styled",
		LMX:                spec.LMX,
	})
	if err != nil {
		return fmt.Errorf("update draft message: %w", err)
	}

	guardian, err := r.API.Guardian(ctx, draft.ID)
	if err != nil {
		return fmt.Errorf("run Guardian: %w", err)
	}
	if len(guardian.Errors) > 0 {
		messages := make([]string, 0, len(guardian.Errors))
		for _, issue := range guardian.Errors {
			messages = append(messages, issue.Rule+": "+issue.Description)
		}
		return fmt.Errorf("Guardian rejected draft: %s", strings.Join(messages, "; "))
	}

	if err := r.API.Publish(ctx, transactional.ID); err != nil {
		return fmt.Errorf("publish draft: %w", err)
	}
	return nil
}
