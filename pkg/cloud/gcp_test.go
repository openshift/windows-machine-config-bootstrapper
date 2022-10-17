package cloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessGCPInstanceHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		expected string
	}{
		{
			name:     "empty hostname",
			hostname: "",
			expected: "",
		},
		{
			name:     "hostname shorter than 63 chars",
			hostname: "gcp-instance-hostname.c.project-identifier.internal",
			expected: "gcp-instance-hostname.c.project-identifier.internal",
		},
		{
			name:     "hostname shorter than 63 chars and no dot",
			hostname: "gcp-instance-hostname",
			expected: "gcp-instance-hostname",
		},
		{
			name:     "hostname with 63 chars",
			hostname: "gcp-instance-hostname-with-63-cha.c.project-identifier.internal",
			expected: "gcp-instance-hostname-with-63-cha.c.project-identifier.internal",
		},
		{
			name:     "hostname longer than 63 chars",
			hostname: "gcp-instance-hostname-with-more-than-63-characters.c.project-identifier.internal",
			expected: "gcp-instance-hostname-with-more-than-63-characters",
		},
		{
			name:     "hostname with first part of the FQDN longer than 63 chars",
			hostname: "gcp-instance-hostname-with-more-than-63-characters-in-the-first-part-of-the-fqdn.c.project-identifier.internal",
			expected: "gcp-instance-hostname-with-more-than-63-characters-in-the-first",
		},
		{
			name:     "hostname longer than 63 chars and no dot",
			hostname: "gcp-instance-hostname-with-more-than-63-characters-and-no-dot-delimiter",
			expected: "gcp-instance-hostname-with-more-than-63-characters-and-no-dot-d",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := processGCPInstanceHostname(test.hostname)
			assert.Equalf(t, test.expected, got, "expected %v, got = %v", test.expected, got)
		})
	}
}
