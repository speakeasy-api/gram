package tailnetgateway

import (
	"fmt"
	"strconv"
	"strings"
)

// maxSlot bounds the StatefulSet ordinal a gateway pod may claim. Slots are
// small integers; anything larger is a misconfiguration.
const maxSlot = 1000

// ParsePodOrdinal returns the StatefulSet ordinal for a pod. The explicit
// value (the apps.kubernetes.io/pod-index label via the downward API) wins;
// otherwise the trailing "-<n>" of the pod name is used.
func ParsePodOrdinal(explicit, podName string) (int32, error) {
	raw := strings.TrimSpace(explicit)
	if raw == "" {
		index := strings.LastIndexByte(podName, '-')
		if index < 0 || index == len(podName)-1 {
			return 0, fmt.Errorf("pod name %q has no ordinal suffix", podName)
		}
		raw = podName[index+1:]
	}
	ordinal, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || ordinal < 0 || ordinal > maxSlot {
		return 0, fmt.Errorf("invalid pod ordinal %q", raw)
	}
	return int32(ordinal), nil
}
