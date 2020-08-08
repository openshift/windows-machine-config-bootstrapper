package wmcb

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
)

// pkgName is the user defined name of the package
type pkgName string

//cniPlugins contains information about the CNI plugin package
type cniPlugins struct {
	pkgInfo
}

//hybridOverlay contains information about the hybrid overlay binary
type hybridOverlay struct {
	pkgInfo
}

// kubeNode contains the information about  the kubernetes node package for Windows
type kubeNode struct {
	pkgInfo
}

// PkgInfo is an interface to populate pkgInfo structs
type PkgInfo interface {
	//getName returns the user defined package name
	getName() pkgName
	//getUrl returns the url of the package
	getUrl() string
	//getShaValue returns SHA value for the package
	getShaValue() (string, error)
	// getShaType returns SHA type (e.g sha256)
	getShaType() string
}

// pkgInfo encapsulates information about a package
type pkgInfo struct {
	// name of the package
	name pkgName
	// url is the URL of the package
	url string
	// sha is the SHA hash of the package
	sha string
	// shaType is the type of SHA used, example: 256 or 512
	shaType string
}

// pkgInfoFactory returns PkgInfo specific to the package name
func pkgInfoFactory(name pkgName, shaType string, baseUrl string, version string) (PkgInfo, error) {
	switch name {
	case cniPluginPkgName:
		return newCniPluginPkg(name, shaType, baseUrl, version)

	default:
		return nil, fmt.Errorf("invalid Package name")
	}
}

// newCniPluginPkg returns cniPlugins implementation of PkgInfo interface
func newCniPluginPkg(name pkgName, shaType string, baseUrl string, version string) (PkgInfo, error) {
	if version == "" {
		return nil, fmt.Errorf("latest cni plugins version not specified")
	}
	if baseUrl == "" {
		return nil, fmt.Errorf("base url for cni plugins not specified")
	}
	return &cniPlugins{
		pkgInfo{
			name:    name,
			url:     baseUrl + version + "/cni-plugins-windows-amd64-" + version + ".tgz",
			shaType: shaType,
		},
	}, nil
}

// getSHAFileContent returns the contents of the SHA file for the given url of the package.
func (p *pkgInfo) getShaFileContent() (string, error) {
	pkgChecksumFileURL := p.url + "." + p.shaType
	response, err := http.Get(pkgChecksumFileURL)
	if err != nil {
		return "", fmt.Errorf("could not fetch content from checksum file: %s", err)
	}
	defer response.Body.Close()

	var checksumFileContent string
	// Fetching checksum file content from the GET Response
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		checksumFileContent = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error in reading file contents")
	}
	return checksumFileContent, nil
}

// getShaValue returns the sha value of the cni plugins package
func (c *cniPlugins) getShaValue() (string, error) {
	if c.sha != "" {
		return c.sha, nil
	}
	checksumFileContent, err := c.getShaFileContent()
	if err != nil {
		return "", err
	}
	// The checksum file content is in the format "<sha> <filename>". So to get SHA we need to extract only the <sha>
	// from the file
	sha512 := strings.Split(checksumFileContent, " ")
	if len(sha512) < 2 {
		return "", fmt.Errorf("checksum file content is not in the format : '<sha> <filename>'")
	}
	c.sha = sha512[0]
	return c.sha, nil
}

// getName returns the user defined name of the package
func (p *pkgInfo) getName() pkgName {
	return p.name
}

//getShaType returns the sha type of the package
func (p *pkgInfo) getShaType() string {
	return p.shaType
}

//getUrl returns the url of the package
func (p *pkgInfo) getUrl() string {
	return p.url
}
