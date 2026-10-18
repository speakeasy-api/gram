package assistants

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func kitMemoryVolume(name, limit string) (corev1.Volume, error) {
	q, err := resource.ParseQuantity(limit)
	if err != nil || q.Sign() <= 0 {
		return corev1.Volume{}, fmt.Errorf("Kit %s requires a positive memory volume size", name)
	}
	return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &q}}}, nil
}
