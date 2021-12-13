package cloud

import (
	"strings"
)

const (
	awsPlatformType = "aws"
)

// GetKubeletHostnameOverride returns correct hostname for kubelet if it should
// be overridden, or an empty string otherwise.
func GetKubeletHostnameOverride(platformType string) (string, error) {
	platformType = strings.ToLower(platformType)
	switch platformType {
	case awsPlatformType:
		return getAWSMetadataHostname()
	default:
		return "", nil
	}
}
