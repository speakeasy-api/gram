package gcp

import (
	"slices"

	storagev1beta1 "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/storage/v1beta1"
)

// storageAPI provisions Cloud Storage buckets through Config Connector.
const storageAPI = "storage.googleapis.com"

func buildStorageValues(subs []DesiredSubscription) *storageValues {
	names := make(map[string]struct{})
	for _, sub := range subs {
		if sub.Storage != nil {
			names[sub.Storage.Bucket] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)

	buckets := make([]storageBucketValue, 0, len(ordered))
	for _, name := range ordered {
		buckets = append(buckets, storageBucketValue{
			Name: name,
			Annotations: map[string]string{
				"cnrm.cloud.google.com/deletion-policy": "abandon",
				"cnrm.cloud.google.com/force-destroy":   "false",
			},
			Labels: map[string]string{"managed_by": managedByLabel},
			Spec: storagev1beta1.StorageBucketSpec{
				PublicAccessPrevention:   new("enforced"),
				UniformBucketLevelAccess: new(true),
			},
		})
	}

	return &storageValues{APIs: []string{storageAPI}, Buckets: buckets}
}
