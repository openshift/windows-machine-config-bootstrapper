package cloud

import (
	"strings"
)

const (
	awsPlatformType = "aws"
	gcpPlatformType = "gcp"
)

// GetKubeletHostnameOverride returns correct hostname for kubelet if it should
// be overridden, or an empty string otherwise.
func GetKubeletHostnameOverride(platformType string) (string, error) {
	platformType = strings.ToLower(platformType)
	switch platformType {
	case awsPlatformType:
		return getAWSMetadataHostname()
	case gcpPlatformType:
		return getGCPMetadataHostname()
	default:
		return "", nil
	}
}
