package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
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
	if instance.ProjectID != thread.ProjectID || !instance.EnvironmentID.Valid {
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
