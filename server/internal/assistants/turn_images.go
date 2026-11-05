package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

const (
	// maxTurnInlineImages caps how many attachment images are inlined into a
	// single turn's input parts.
	maxTurnInlineImages = 4
	// maxTurnInlineImageBytes caps the total pre-base64 bytes of inlined
	// images per turn. Each individual file is additionally capped at
	// slackapi.MaxImageFileBytes by the downloader.
	maxTurnInlineImageBytes = 8 * 1024 * 1024
)

// slackImageFetcher is the downloader surface used to inline trigger-message
// images; satisfied by *slackapi.Client.
type slackImageFetcher interface {
	FetchImageFile(ctx context.Context, fileID string, token string) (*slackapi.ImageFile, error)
}

// SpeakeasySlackTokens resolves the bot token of the Slack workspace whose
// Speakeasy app answers for a project.
type SpeakeasySlackTokens interface {
	ProjectBotToken(ctx context.Context, organizationID string, projectID uuid.UUID) (string, error)
}

// SetSpeakeasySlackTokens wires the token source for Speakeasy Slack app
// turns.
func (s *ServiceCore) SetSpeakeasySlackTokens(tokens SpeakeasySlackTokens) {
	s.speakeasySlack = tokens
}

// speakeasySlackRouted reports whether a connected workspace routes the
// Speakeasy Slack app to the assistant's project and can post right now. A
// workspace that lost chat:write or usable credentials does not count, so its
// Slack tools are not advertised.
func (s *ServiceCore) speakeasySlackRouted(ctx context.Context, assistant assistantRecord) (bool, error) {
	if s.speakeasySlack != nil {
		_, err := s.speakeasySlack.ProjectBotToken(ctx, assistant.OrganizationID, assistant.ProjectID)
		if errors.Is(err, slackdirectoryconnections.ErrNoRoutingWorkspace) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("resolve Speakeasy Slack app workspace: %w", err)
		}
		return true, nil
	}
	_, err := slackrepo.New(s.db).GetSlackRoutingConnectionByProject(ctx, slackrepo.GetSlackRoutingConnectionByProjectParams{
		OrganizationID:   assistant.OrganizationID,
		DefaultProjectID: uuid.NullUUID{UUID: assistant.ProjectID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve Speakeasy Slack app workspace: %w", err)
	}
	return true, nil
}

// SetSlackImageInlining wires the environment loader and Slack file
// downloader used to inline image attachments from triggering Slack messages
// as vision content. Set after construction to match the existing
// post-construction injection pattern; when unset, turns carry attachment
// metadata only.
func (s *ServiceCore) SetSlackImageInlining(envLoader toolconfig.EnvironmentLoader, fetcher slackImageFetcher) {
	s.envLoader = envLoader
	s.slackImages = fetcher
}

// slackTurnImageParts downloads the image attachments of a Slack trigger
// event and renders them as image_url input parts with data: URIs. Strictly
// best-effort: every failure logs and degrades to nil — the turn's
// message-context block already tells the model the files exist and that the
// Slack platform tools can reach them.
func (s *ServiceCore) slackTurnImageParts(ctx context.Context, thread assistantThreadRecord, event assistantThreadEventRecord) []runtimeContentPart {
	if s.envLoader == nil || s.slackImages == nil || !event.TriggerInstanceID.Valid {
		return nil
	}

	var payload slackEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return nil
	}
	candidates := imageFileCandidates(payload.Files)
	if len(candidates) == 0 {
		return nil
	}

	token, err := s.slackTriggerToken(ctx, thread, event)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve slack token for turn image inlining; sending metadata only",
			attr.SlogError(err), attr.SlogAssistantThreadID(thread.ID.String()))
		return nil
	}
	if token == "" {
		return nil
	}

	return fetchInlineImageParts(ctx, s.logger, s.slackImages, token, candidates)
}

// slackTriggerToken resolves the Slack token from the trigger instance's
// bound environment, mirroring the platform tools' bot-first preference.
// Returns "" (no error) when the instance has no environment or the
// environment carries no Slack token.
func (s *ServiceCore) slackTriggerToken(ctx context.Context, thread assistantThreadRecord, event assistantThreadEventRecord) (string, error) {
	instance, err := triggerrepo.New(s.db).GetTriggerInstanceByIDPublic(ctx, event.TriggerInstanceID.UUID)
	if err != nil {
		return "", fmt.Errorf("load trigger instance for turn image inlining: %w", err)
	}
	if instance.ProjectID != thread.ProjectID {
		return "", nil
	}
	if instance.DefinitionSlug == sourceKindSpeakeasySlack {
		if s.speakeasySlack == nil {
			return "", nil
		}
		token, err := s.speakeasySlack.ProjectBotToken(ctx, instance.OrganizationID, instance.ProjectID)
		if err != nil {
			return "", fmt.Errorf("resolve Speakeasy Slack app token for turn image inlining: %w", err)
		}
		return token, nil
	}
	if !instance.EnvironmentID.Valid {
		return "", nil
	}
	envMap, err := s.envLoader.Load(ctx, thread.ProjectID, toolconfig.ID(instance.EnvironmentID.UUID))
	if err != nil {
		return "", fmt.Errorf("load trigger environment for turn image inlining: %w", err)
	}
	token, err := slackapi.TokenFromEnv(slackapi.TokenPreferBot, toolconfig.CIEnvFrom(envMap))
	if err != nil {
		return "", nil
	}
	return token, nil
}

// imageFileCandidates filters attachment metadata down to declared images
// and keeps at most maxTurnInlineImages, preferring the trailing (newest)
// entries of the message's file list. The declared mimetype is a hint only;
// the downloader re-validates content by magic bytes.
func imageFileCandidates(files []slackFilePayload) []slackFilePayload {
	images := make([]slackFilePayload, 0, len(files))
	for _, file := range files {
		if file.ID != "" && strings.HasPrefix(file.Mimetype, "image/") {
			images = append(images, file)
		}
	}
	if len(images) > maxTurnInlineImages {
		images = images[len(images)-maxTurnInlineImages:]
	}
	return images
}

// fetchInlineImageParts downloads the candidates concurrently — the fetches
// share no state, so turn dispatch pays one file's latency instead of the
// sum — then applies the per-turn byte budget in message order, so
// concurrency cannot reorder which files get admitted. A file that fails to
// download or validate is skipped, never fatal.
func fetchInlineImageParts(ctx context.Context, logger *slog.Logger, fetcher slackImageFetcher, token string, files []slackFilePayload) []runtimeContentPart {
	images := make([]*slackapi.ImageFile, len(files))
	var eg errgroup.Group
	for i, file := range files {
		if file.Size > 0 && file.Size > maxTurnInlineImageBytes {
			continue
		}
		eg.Go(func() error {
			img, err := fetcher.FetchImageFile(ctx, file.ID, token)
			if err != nil {
				logger.WarnContext(ctx, "inline slack turn image; skipping file", attr.SlogError(err))
				return nil
			}
			images[i] = img
			return nil
		})
	}
	// Workers never return errors; failures degrade to metadata-only.
	_ = eg.Wait()

	remaining := int64(maxTurnInlineImageBytes)
	parts := make([]runtimeContentPart, 0, len(files))
	for _, img := range images {
		if img == nil || int64(len(img.Data)) > remaining {
			continue
		}
		remaining -= int64(len(img.Data))
		parts = append(parts, runtimeContentPart{
			Type: contentPartTypeImageURL,
			Text: "",
			ImageURL: &runtimeImageURL{
				URL:    img.DataURI(),
				Detail: "",
			},
		})
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
