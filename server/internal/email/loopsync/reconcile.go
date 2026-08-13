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
		transactional, created, err := r.resolve(ctx, key, spec, existingIDs[key], byName)
		if err != nil {
			return nil, err
		}
		resolved[key] = transactional.ID

		changed, err := r.syncOne(ctx, manifest.Defaults, spec, transactional, created)
		if err != nil {
			return nil, fmt.Errorf("sync email template %q: %w", key, err)
		}
		if r.Log != nil {
			status := "unchanged"
			if changed {
				status = "published"
			}
			_, _ = fmt.Fprintf(r.Log, "%s: %s\n", key, status)
		}
	}
	return resolved, nil
}

func (r *Reconciler) resolve(ctx context.Context, key string, spec TemplateSpec, mappedID string, byName map[string][]TransactionalEmail) (TransactionalEmail, bool, error) {
	if mappedID != "" {
		transactional, err := r.API.GetTransactionalEmail(ctx, mappedID)
		if err != nil {
			return TransactionalEmail{}, false, fmt.Errorf("resolve mapped email template %q: %w", key, err)
		}
		if transactional.Name != spec.ManagedName {
			return TransactionalEmail{}, false, fmt.Errorf("resolve mapped email template %q: ID %q belongs to %q, want %q", key, mappedID, transactional.Name, spec.ManagedName)
		}
		return transactional, false, nil
	}

	matches := byName[spec.ManagedName]
	if len(matches) > 1 {
		return TransactionalEmail{}, false, fmt.Errorf("resolve email template %q: found %d Loops emails named %q", key, len(matches), spec.ManagedName)
	}
	if len(matches) == 1 {
		return matches[0], false, nil
	}

	transactional, err := r.API.CreateTransactionalEmail(ctx, spec.ManagedName)
	if err != nil {
		return TransactionalEmail{}, false, fmt.Errorf("create email template %q: %w", key, err)
	}
	return transactional, true, nil
}

func (r *Reconciler) syncOne(ctx context.Context, defaults MessageDefaults, spec TemplateSpec, transactional TransactionalEmail, created bool) (bool, error) {
	if !created && transactional.PublishedEmailMessageID != nil {
		published, err := r.API.GetEmailMessage(ctx, *transactional.PublishedEmailMessageID)
		if err != nil {
			return false, fmt.Errorf("get published message: %w", err)
		}
		if sameContent(published, defaults, spec) {
			if err := r.verifyPublishedVariables(ctx, transactional.ID, spec); err != nil {
				return false, err
			}
			return false, nil
		}
	}

	transactional, err := r.API.EnsureDraft(ctx, transactional.ID)
	if err != nil {
		return false, fmt.Errorf("ensure draft: %w", err)
	}
	if transactional.DraftEmailMessageID == nil {
		return false, fmt.Errorf("ensure draft: Loops returned no draft email message ID")
	}

	draft, err := r.API.GetEmailMessage(ctx, *transactional.DraftEmailMessageID)
	if err != nil {
		return false, fmt.Errorf("get draft message: %w", err)
	}
	if !sameContent(draft, defaults, spec) {
		draft, err = r.API.UpdateEmailMessage(ctx, draft.ID, UpdateEmailMessageInput{
			ExpectedRevisionID: draft.ContentRevisionID,
			Subject:            spec.Subject,
			PreviewText:        spec.PreviewText,
			FromName:           defaults.FromName,
			FromEmail:          defaults.FromEmail,
			ReplyToEmail:       defaults.ReplyToEmail,
			LMX:                spec.LMX,
		})
		if err != nil {
			return false, fmt.Errorf("update draft message: %w", err)
		}
	}

	guardian, err := r.API.Guardian(ctx, draft.ID)
	if err != nil {
		return false, fmt.Errorf("run Guardian: %w", err)
	}
	if len(guardian.Errors) > 0 {
		messages := make([]string, 0, len(guardian.Errors))
		for _, issue := range guardian.Errors {
			messages = append(messages, issue.Rule+": "+issue.Description)
		}
		return false, fmt.Errorf("Guardian rejected draft: %s", strings.Join(messages, "; "))
	}

	if _, err := r.API.Publish(ctx, transactional.ID); err != nil {
		return false, fmt.Errorf("publish draft: %w", err)
	}
	if err := r.verifyPublishedVariables(ctx, transactional.ID, spec); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Reconciler) verifyPublishedVariables(ctx context.Context, transactionalID string, spec TemplateSpec) error {
	published, err := r.API.GetTransactionalEmail(ctx, transactionalID)
	if err != nil {
		return fmt.Errorf("verify published email: %w", err)
	}
	want := slices.Clone(spec.PublishedVariables)
	got := slices.Clone(published.DataVariables)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("published data variable contract is %v, want %v", got, want)
	}
	return nil
}

func sameContent(message EmailMessage, defaults MessageDefaults, spec TemplateSpec) bool {
	return message.Subject == spec.Subject &&
		message.PreviewText == spec.PreviewText &&
		message.FromName == defaults.FromName &&
		message.FromEmail == defaults.FromEmail &&
		message.ReplyToEmail == defaults.ReplyToEmail &&
		strings.TrimSpace(message.LMX) == spec.LMX
}
