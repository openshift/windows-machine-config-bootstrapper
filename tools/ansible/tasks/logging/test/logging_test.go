package test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Making this a map so that if we want a specific version, the value can be the version
var requiredPluginList = map[string]string{
	// Pinning down version of Fluentd to 1.5
	"fluentd":                 "1.5",
	"elasticsearch-transport": "",
	"elasticsearch-api":       "",
	"elasticsearch":           "",
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

// TestRubyInstalled tests if Ruby is installed correctly
// This test should be run on a node after the logging playbook has been run against it
func TestRubyInstalled(t *testing.T) {
	// Pinning down version of Ruby Devkit to 2.5.7, 64 bit version
	// Ruby installation path assumed to be default
	var rubyExecutablePath = "C:\\Ruby25-x64\\bin\\ruby.exe"
	var rubyVersion = "2.5.7"
	// exec.Command requires fully qualified path for ruby.exe
	out, err := exec.Command(rubyExecutablePath, "--version").Output()
	require.NoError(t, err)
	// Grabbing the version of Ruby in the format 2.5.7
	actualVersion := strings.Split(string(out), " ")[1][:5]
	assert.Equalf(t, actualVersion, rubyVersion, "Ruby expected version %s, got %s", rubyVersion, actualVersion)
}

// TestPluginsInstalled tests that the fluentd and related plugins were all installed correctly.
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
			// Test version specific to fluentd
			if pluginName == "fluentd" {
				// Matching only upto the minor version (ex 1.5), not the patch (ex 1.5.1) of Fluentd because
				// current gem install of fluentd is upgrading to the latest patch
				assert.Equalf(t, pluginVersion, installedVersion[:3],
					"%s expected version: %s, got: %s", pluginName, pluginVersion, installedVersion)
			} else {
				assert.Equalf(t, pluginVersion, installedVersion,
					"%s expected version: %s, got: %s", pluginName, pluginVersion, installedVersion)
			}
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
