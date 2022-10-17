package cloud

import (
	"strings"

	"cloud.google.com/go/compute/metadata"
	"github.com/pkg/errors"
)

// gcpHostnameMaxLength is the maximum number of character allowed for the instance's hostname in GCP
const gcpHostnameMaxLength = 63

// processGCPInstanceHostname truncates the given hostname if the length is longer 63 characters, otherwise returns
// the original value
func processGCPInstanceHostname(hostname string) string {
	// check hostname length
	if len(hostname) > gcpHostnameMaxLength {
		// check if hostname shorter than 63 characters before the first dot in the FQDN
		firstDotIndex := strings.Index(hostname, ".")
		if firstDotIndex > 0 && firstDotIndex < gcpHostnameMaxLength {
			// return first part of the FQDN
			return hostname[:firstDotIndex]
		}
		return hostname[:gcpHostnameMaxLength]
	}
	// return original value
	return hostname
}

// getGCPMetadataHostname returns the GCP instance hostname from the metadata service.
// For example "<instanceName>.c.<projectID>.internal". The resulting hostname is truncated if the length is longer
// than 63 characters.
func getGCPMetadataHostname() (string, error) {
	hostname, err := metadata.Hostname()
	if err != nil {
		return "", errors.Wrap(err, "unable to retrieve the hostname from GCP instance metadata service")
	}
	return processGCPInstanceHostname(hostname), nil
}
