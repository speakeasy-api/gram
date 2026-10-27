package assistants

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func kitMemoryVolume(name, limit string) (corev1.Volume, error) {
	q, err := resource.ParseQuantity(limit)
	if err != nil || q.Sign() <= 0 {
		return corev1.Volume{}, fmt.Errorf("kit %s requires a positive memory volume size", name)
	}
	var source corev1.VolumeSource
	source.EmptyDir = &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &q}
	return corev1.Volume{Name: name, VolumeSource: source}, nil
}
