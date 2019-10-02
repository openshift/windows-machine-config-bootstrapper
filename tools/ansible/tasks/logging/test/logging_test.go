package test

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os/exec"
	"strings"
	"testing"
)

// Making this a map so that if we want a specific version, the value can be the version
var requiredPluginList = map[string]string{
	"elasticsearch-transport":                  "",
	"elasticsearch-api":                        "",
	"elasticsearch":                            "",
	"fluent-plugin-kubernetes_metadata_filter": "",
	"fluent-plugin-concat":                     "",
	"fluent-plugin-elasticsearch":              "",
	"fluent-plugin-multi-format-parser":        "",
	"fluent-plugin-record-modifier":            "",
	"fluent-plugin-rewrite-tag-filter":         "",
	"fluent-plugin-systemd":                    "",
	"fluent-plugin-viaq_data_model":            "",
	"fluent-plugin-remote-syslog":              "",
	"fluent-plugin-prometheus":                 "",
}

// TestPluginsInstalled tests that the fluentd plugins were all installed correctly.
// This test should be run on a node after the logging playbook has been run against it
func TestPluginsInstalled(t *testing.T) {
	out, err := exec.Command("fluent-gem", "list").Output()
	require.NoError(t, err)
	installedPlugins := parseInstalledFluentdPlugins(string(out))

	// Compare the installed plugins and the desired plugins
	for pluginName, pluginVersion := range requiredPluginList {
		installedVersion, ok := installedPlugins[pluginName]
		assert.Truef(t, ok, "Missing plugin %s", pluginName)
		if pluginVersion != "" {
			assert.Equalf(t, pluginVersion, installedVersion,
				"%s expected version: %s, got: %s", pluginName, pluginVersion, installedVersion)
		}
	}
}

// parseInstalledFluentdPlugins returns a list of plugins installed, given a string with lines of the format
// `plugin_name (version)`. For example:
// bigdecimal (default: 1.3.2)
func parseInstalledFluentdPlugins(s string) map[string]string {
	installedPlugins := map[string]string{}
	lines := strings.Split(string(s), "\n")
	for _, l := range lines {
		// Each line has a name and a version, so split them into two substrings
		sp := strings.SplitN(l, " ", 2)
		if len(sp) > 1 {
			name := sp[0]
			// We need to remove the parenthesis surrounding the version
			version := sp[1][1 : len(sp[1])-2]
			installedPlugins[name] = version
		}
	}
	return installedPlugins
}
