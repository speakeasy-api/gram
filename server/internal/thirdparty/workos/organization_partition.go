package workos

import (
	"context"
	"errors"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/organizations"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

// PartitionMetadataKey is the WorkOS organization metadata key that records
// which partition owns the organization. It is not prefixed with an
// application name because every Speakeasy application shares the WorkOS
// environment and the marker describes the organization, not one application.
// An organization without it belongs to [partition.US], so organizations
// created before partitions existed stay where they are. external_id is not
// used for this because it holds the Speakeasy organization ID.
const PartitionMetadataKey = "partition"

// ErrInvalidPartitionMarker reports an organization whose partition marker is
// present but is not exactly one of the known partition names. Callers must
// not treat such an organization as belonging to any partition.
var ErrInvalidPartitionMarker = errors.New("invalid WorkOS organization partition marker")

// ErrClientPartitionNotConfigured reports an organization create on a client
// built without a valid partition. Creating the organization anyway would
// leave it unmarked, which means US.
var ErrClientPartitionNotConfigured = errors.New("WorkOS client has no valid partition configured")

// PartitionFromMetadata returns the partition recorded in WorkOS organization
// metadata. A missing marker means [partition.US]. Any other value must match
// a partition name exactly; anything else returns an error wrapping
// [ErrInvalidPartitionMarker].
func PartitionFromMetadata(metadata map[string]string) (partition.Partition, error) {
	raw, ok := metadata[PartitionMetadataKey]
	if !ok {
		return partition.US, nil
	}

	p, err := partition.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPartitionMarker, err)
	}

	return p, nil
}

// Partition returns the partition that owns the organization. A missing
// marker means [partition.US]; a marker that is not a known partition name
// returns an error wrapping [ErrInvalidPartitionMarker] that names the
// organization.
func (o Organization) Partition() (partition.Partition, error) {
	p, err := PartitionFromMetadata(o.Metadata)
	if err != nil {
		return "", fmt.Errorf("workos organization %s: %w", o.ID, err)
	}

	return p, nil
}

// SetOrganizationPartition changes the partition marker of a WorkOS
// organization. Only the marker key is sent, so other metadata keys are left
// as they are. The partition must be a known partition name.
func (wc *Client) SetOrganizationPartition(ctx context.Context, workosOrgID string, p partition.Partition) error {
	if _, err := partition.Parse(p.String()); err != nil {
		return fmt.Errorf("set organization partition: %w", err)
	}

	value := p.String()
	_, err := wc.orgs.UpdateOrganization(ctx, organizations.UpdateOrganizationOpts{ //nolint:exhaustruct_v5 // deprecated WorkOS fields are intentionally omitted.
		Organization:     workosOrgID,
		Name:             "",
		Domains:          nil,
		DomainData:       nil,
		ExternalID:       "",
		StripeCustomerID: "",
		// Never send an empty map here: WorkOS treats metadata {} as deleting
		// every key.
		Metadata: map[string]*string{PartitionMetadataKey: &value},
	})
	if err != nil {
		return fmt.Errorf("set organization partition: %w", err)
	}

	return nil
}

// partitionMetadata returns the metadata that marks a new organization as
// belonging to this client's partition. It fails when the client has no valid
// partition, so no organization is created unmarked.
func (wc *Client) partitionMetadata() (map[string]string, error) {
	if wc.partition == "" {
		return nil, ErrClientPartitionNotConfigured
	}

	p, err := partition.Parse(wc.partition.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrClientPartitionNotConfigured, err)
	}

	return map[string]string{PartitionMetadataKey: p.String()}, nil
}
