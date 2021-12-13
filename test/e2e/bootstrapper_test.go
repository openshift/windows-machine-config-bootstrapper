package e2e

import (
	"fmt"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/util/wait"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var ignitionFilePath string
var kubeletPath string
var installDir string
var platformType string

const (
	kubeletLogPath = "C:\\var\\log\\kubelet\\kubelet.log"
	// pollIntervalKubeletLog is the interval at which we poll the kubelet log
	pollIntervalKubeletLog = 30 * time.Second
	// waitTimeKubeletLog is the maximum duration to get kubelet log
	waitTimeKubeletLog = 2 * time.Minute
)

// This default ignition file will not connect to a living cluster
var defaultIgnitionFileContents = `{"ignition":{"version":"3.1.0"},"passwd":{"users":[{"name":"core","sshAuthorizedKeys":["ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDUq7W38xCZ9WGSWCvustaMGMT04tRohw6AKGzI7P7xql5lhCAReyt72n9qWQRZsE1YiCSQuTfXI1oc8NpSM7+lMLwj12G8z3I1YT31JHr9LLYg/XIcExkzfBI920CaS82VqmKOpI9+ARHSJBdIbKRI0f5Y+u4xbc5UzKCJX8jcKGG7nEiw8zm+cvAlfOgssMK+qJppIbVcb2iZNTsw5i2aX6FDMyC+b17DQHzBGpNbhZYxuoERZVRcnYctgIzuo6fD60gniX0fVvrchlOnubB1sRYbloP2r6UE22w/dpLKOFE5i7CA0ZzNBERZ94cIKumIH9MiJs1a6bMe89VOjjNV\n"]}]},"storage":{"files":[{"path":"/etc/kubernetes/kubelet-ca.crt","contents":{"source":"data:,-----BEGIN%20CERTIFICATE-----%0AMIIDMDCCAhigAwIBAgIIRApfIffG4fcwDQYJKoZIhvcNAQELBQAwNjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSAwHgYDVQQDExdhZG1pbi1rdWJlY29uZmlnLXNpZ25lcjAe%0AFw0xOTA5MTgxNDUzMjJaFw0yOTA5MTUxNDUzMjJaMDYxEjAQBgNVBAsTCW9wZW5z%0AaGlmdDEgMB4GA1UEAxMXYWRtaW4ta3ViZWNvbmZpZy1zaWduZXIwggEiMA0GCSqG%0ASIb3DQEBAQUAA4IBDwAwggEKAoIBAQDWAhl7ECtHpFHKKRhOez8ydzLTeUiG86kb%0An%2BiSeK7MeRHxiYIokPVkGrS3OLD1yxXQa5R5BnJxR9yzDmJK4BEQKyTFQzTd6xbZ%0ABrXAjy6MiUTfhM0Ke0bA71oIM65%2F6766csUAbebeceejkSR9u4%2BifmxltTbBbKTB%0AezYspx%2FXU1%2FhO9tH1wXSwdO3qi%2F12TYIzlw3a%2BrgdfJSKq0MoNEXfz75M2gNDWVA%0AQY6ssLu7SO4G3RbhsuR%2BQFsPd7ZxdiYijbuNV8XA9O6UbteJmQDo%2FvEyi9xafCgr%0A5pJn0g3RPqPsWowQrNh6b8MJWNC%2F2syqK0tf6H1SRKH394rjtUzrAgMBAAGjQjBA%0AMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMBAf8EBTADAQH%2FMB0GA1UdDgQWBBRgqIAg%0ADdPtxXzGkslXRzTZdVyeWzANBgkqhkiG9w0BAQsFAAOCAQEArqflTKTwAMjqlnR%2B%0AESEHb3HQvNVggcvvOAbYzw2dThLM0OtyxdcEd5b2ZOYxbKp1Zh5VjoIIzenq5gtj%0A1iogAA1ZOjYabNeOUjovlIcou2%2F8bpRH1zCO%2FN4MrkPjdXRBvs65uFdZ8AlOVYGc%0ApGI4uoeJFbZNP2t9DbqpsWstYW5j8Rz2aa2M2Jujvz0Bt7wln2bCr5BmUiL5cLS%2F%0Are7AA97vK7XWQYxeNKh%2BgVT5Of3vcC%2BPehzTrPt%2FMTKAJK6YTuFdhsYTRr6lmor7%0Ay6tyRd64FIAYFXGxwwUwcshuu%2BnAE1XvIwSyN0KXmi0kAXLj7EXCVWvgVnhlltZn%0A1jSW7w%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDHjCCAgagAwIBAgIIFZCGpKYvDSMwDQYJKoZIhvcNAQELBQAwLTESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRcwFQYDVQQDEw5rdWJlbGV0LXNpZ25lcjAeFw0xOTA5MTgx%0ANDUzMjdaFw0xOTA5MTkxNDUzMjdaMC0xEjAQBgNVBAsTCW9wZW5zaGlmdDEXMBUG%0AA1UEAxMOa3ViZWxldC1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEK%0AAoIBAQDNafBP4Bz3dryVxvRarO%2FIaVwc%2BahbUrqLVciK1aYULuCcUllicR%2FCGgvg%0AMTRBrvYZCghk8%2FOLLmDC4%2BQzYUlwh4orbEfHno8ssTEciNxSI1phOrtibM9vR6hh%0APBY%2BifqThn2AkIy%2FT3ZOrXqCZzEaSgWBtjlaYdE2nV4SM0RLq7iu93WxNdFvzBYh%0A6n3%2BWIn5M0IjYacJzHmCN5FP5l5TOEU0hPWi2xOCzjJpTW%2FWPq6ZqaubwuLtlK82%0AY%2FaO5ABn1wjjvEfXUCz6vzmx31ST1cmhNdJD0zZMsdOpGfGxULW%2BNCs8KzcYo%2BdC%0A9LpC3SLGHegPICAifu5gh8B1EsSPAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAP%0ABgNVHRMBAf8EBTADAQH%2FMB0GA1UdDgQWBBTYVxXl1CAOAItVVeff%2F%2F4DUAPXtDAN%0ABgkqhkiG9w0BAQsFAAOCAQEAWuwz%2Fx%2BguIS8IlavJKjNQ7zL849eRPaf%2BXF7Ie0h%0AdYi8wRW9WxRPRnt3%2BqHY%2BQAOz2eaF%2Bq7JchiMRhJb9%2F704fZG%2FsQZ6xb7f1l81vz%0Al1zVf9XAFNeE%2F02oPWj5eDSxEvjcim1FkQpaQ7e3zQ15hwWeGKkQIP91CpXrEsfy%0AxCw%2BZygxIt7lankDV%2BPTG%2BUqBqkKNfaSLUvJpoa%2BdNZFX5dKCMEBrERm4CAni%2Fyj%0AbNs5ZBSliDrJVc1rg7YEjVpID%2BH6AQi86%2Fw2xwH0kEhG%2B%2FNbE4a6FT9RBXTH2lqY%0AizF%2FFwcDAfW2gchAQ3Wg8p53sb2nNXrCWAdiTyCpW%2FpF0w%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDNDCCAhygAwIBAgIIVShd3S3Ot9gwDQYJKoZIhvcNAQELBQAwODESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSIwIAYDVQQDExlrdWJlLWNvbnRyb2wtcGxhbmUtc2lnbmVy%0AMB4XDTE5MDkxODE0NTMyN1oXDTIwMDkxNzE0NTMyN1owODESMBAGA1UECxMJb3Bl%0AbnNoaWZ0MSIwIAYDVQQDExlrdWJlLWNvbnRyb2wtcGxhbmUtc2lnbmVyMIIBIjAN%0ABgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA41IIjDIHyMPhE8cNCPtM58o5eatd%0A4wU7Opd54X4u8hhHvcJVhUR7Oh3L8AtFGp%2FenemVSF667ncfUqohKv0nFjbgIWEx%0AwjXuTz59xLu7dM93PoQqpjJ8C9PURXUD9KICS03pgDbEPLgzO4uZNpQ0XsYQzMk5%0A4RtiG9YJwj1QxMc2DCkuJePPqzaOPM6zl2Ju1VIIhtHz71n4apq5rsOgRDEa81xz%0AUCd0bXhsFFTXyIs6Pjzp31LMeleIaxolTkh8yByHJHYdn9LAr7Oplw00%2F391gfqz%0AZ4bq4hss2LR6fN1CUBmhGYvkKpD5p0wUVw%2BxLAhDPjqex7hnO39PIXaQbwIDAQAB%0Ao0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%2FzAdBgNVHQ4EFgQU%0ATmrzA5XUbU2Nvw%2FoHEhyb5lg6jgwDQYJKoZIhvcNAQELBQADggEBAKPtsGtoAK5Y%0AxhwTRS7oGpNtK5c5jR8AKzLemuY5BGVeK%2BzQu4KwTPHc4uyUf7V7vDTOq9CuLeej%0AAjJtCazB2uqG7U6M7H8AejL0ATm4CbFAQswfOcmNFDcz9QwYrBCPXQ5oFvBu%2Fics%0AqvnSnx%2BzYXcJLImpdVR%2BulBsxAuXa3fcct7IBC5Ysjeiq39txfbXnry9O6c7Lcy0%0AO%2FMm%2FWqC5UczDEKW9ZwljNi%2FofGLxmuFskpnD2CJTe8%2BdelGPNFqqFvhP3wzj429%0A%2Bw%2F8jCDxDEZokiW31AA4TVO9mf9YxWuWGOSQBsdJdx6B%2Fb9%2F0bB0UL1RYtiSYNMj%0A2C%2FkUvMUpB0%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDQjCCAiqgAwIBAgIIP4nZ4FYJQ0gwDQYJKoZIhvcNAQELBQAwPzESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSkwJwYDVQQDEyBrdWJlLWFwaXNlcnZlci10by1rdWJlbGV0%0ALXNpZ25lcjAeFw0xOTA5MTgxNDUzMjdaFw0yMDA5MTcxNDUzMjdaMD8xEjAQBgNV%0ABAsTCW9wZW5zaGlmdDEpMCcGA1UEAxMga3ViZS1hcGlzZXJ2ZXItdG8ta3ViZWxl%0AdC1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCe2dZh2d66%0AIDK6dFpY9bSHVVUkKX6AAxlXakPs1cr9pKS7A9j%2FM980NhOwY6oYQqNPxHRQ4FCZ%0AHADXTXscRHf1B7aGe5j2QvTsx2F%2F8enK%2BVwTK3Z6tcZdhajPaNEFle6xgNS%2B%2F8QZ%0AY4Y8KgWA817vH8RGjgaFcEWfwX%2BzQ0ULgqO6MOCbF0MStKRzPTBOF10HOqBgl9Qt%0AaXsM47lMDIKaDP5L1rOq9xy7nc50wBkRMkR9SSo4FE5oynZZe%2FKjIMIGQxzcehd3%0AZvtI8n9iPvCkBYvTRpXc1DYrubffUJbEFOGyw7Nns1yRbO4RszDl2IllPGbThBq1%0AbDO%2BXl2XgxpfAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMBAf8EBTAD%0AAQH%2FMB0GA1UdDgQWBBTj9aK1YBedBuFwrD221rZdXnFUIjANBgkqhkiG9w0BAQsF%0AAAOCAQEAnrD5EMXuJWU%2F8j9c5M0%2FN2e69xZ9EDfKvg%2BDzeg2wJmefS%2BgWIUuBbDm%0AlKeidso8Kc0mPv9hTzx1uKipR0Zt1HfeYI8v3HJy0O79owKAHesQUxdGMzOF2L%2BD%0AbSEbhcajvAX7hBKRvj1dHLKeHaudTo8r%2FCxKfvXjh5FECPo5yXYWoodLL7ktYfLZ%0APRNiEkmp0VzhNI6ibccbe6f78eIwr6EJQcpOlFAmG4iPsqEgMy3JvTmFFgzVDSPd%0ANLQp9Hmj4%2B552WD4%2FVX44Q%2BhHZO%2BJ%2B9Ti5QbeY%2BIwGrS36CQDHiTRnLAIlyX%2Bd2B%0AefJuJfR6iCBudlGGJtIA8AORn7ZoPw%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDSDCCAjCgAwIBAgIIWv2MTEp15xEwDQYJKoZIhvcNAQELBQAwQjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSwwKgYDVQQDEyNrdWJlbGV0LWJvb3RzdHJhcC1rdWJlY29u%0AZmlnLXNpZ25lcjAeFw0xOTA5MTgxNDUzMjNaFw0yOTA5MTUxNDUzMjNaMEIxEjAQ%0ABgNVBAsTCW9wZW5zaGlmdDEsMCoGA1UEAxMja3ViZWxldC1ib290c3RyYXAta3Vi%0AZWNvbmZpZy1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDM%0ARLgkYUNWZgrvqPRBwdsqGFBbv1CkNLpuGtQt%2FJIh%2Bf5Ha%2F9fhsbFSfjs2T%2FkBiwH%0AEEqjaiwhIcN%2BsxvubKJoJ%2B9zGyB%2F0hi7eCfhg53Tgx7zAV4SxIA%2Fb2ycoirBk%2FTU%0Adwt4mkZ%2FR9Cgwqcwq9FL2t9eDr5DCOgAkARrBNGxonWf96rHxW%2BvurHNC2yw3kFX%0Au6JoHtkIzB10Ob4cC3lO9b5VIatdcYIKQw1LOZwawU%2Ffo9%2F2HGG10J%2BiTRl2Lhoa%0A9V%2Fvqm9xY1vNM3t8%2Fsl2ABK%2FTYbzcTgqWRJoEVFFaUhqWu%2F%2Fg3miV4deGnlRBEXP%0AGOJ7mzD1ZmhNnZA1PetZAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMB%0AAf8EBTADAQH%2FMB0GA1UdDgQWBBSj0TLepVBgOsp%2BTw2LzJsGB7iY0jANBgkqhkiG%0A9w0BAQsFAAOCAQEAJ3kANn3dkCmicvEjK%2FwhwT6eeI6IK2CSJy3pBDw9pn0Xmrt1%0AT9Hpj0NQvPVEq%2Bdw0utsXsMU6NmyCJ3n7PowQvXhbwOG5n4ATNX19Sih4dFihafa%0ATT0s0gMO4GIGFmEr8ew0kmA85ddrFtHtuMKTpk6IzhaLzx5Cu3BWxqCf1dxHEJHD%0A%2BOqSNJpERbmwPlDBEkqlzXc359U8ZTeoBE%2Bu8h9HmKB5MOlNd9P8TyryPXrQCdhZ%0AthdvOZcR9P1Ovw4YXs6pX4R5CpYi3Q%2FuvT77Wq3U7Ma%2FsklYnDSRshAeSj1FWgx1%0Ai4bO7%2F4OJ5%2B7enK3OINB%2B5%2FAKEMUgj6DecHi2A%3D%3D%0A-----END%20CERTIFICATE-----%0A"},"mode":420},{"path":"/etc/kubernetes/kubelet.conf","contents":{"source":"data:,kind%3A%20KubeletConfiguration%0AapiVersion%3A%20kubelet.config.k8s.io%2Fv1beta1%0Aauthentication%3A%0A%20%20x509%3A%0A%20%20%20%20clientCAFile%3A%20%2Fetc%2Fkubernetes%2Fkubelet-ca.crt%0A%20%20anonymous%3A%0A%20%20%20%20enabled%3A%20false%0AcgroupDriver%3A%20systemd%0AclusterDNS%3A%0A%20%20-%20172.30.0.10%0AclusterDomain%3A%20cluster.local%0AcontainerLogMaxSize%3A%2050Mi%0AmaxPods%3A%20250%0ArotateCertificates%3A%20true%0AruntimeRequestTimeout%3A%2010m%0AserializeImagePulls%3A%20false%0AstaticPodPath%3A%20%2Fetc%2Fkubernetes%2Fmanifests%0AsystemReserved%3A%0A%20%20cpu%3A%20500m%0A%20%20memory%3A%20500Mi%0AfeatureGates%3A%0A%20%20RotateKubeletServerCertificate%3A%20true%0A%20%20ExperimentalCriticalPodAnnotation%3A%20true%0A%20%20SupportPodPidsLimit%3A%20true%0A%20%20LocalStorageCapacityIsolation%3A%20false%0AserverTLSBootstrap%3A%20true%0A"},"mode":420},{"path":"/etc/kubernetes/ca.crt","contents":{"source":"data:,-----BEGIN%20CERTIFICATE-----%0AMIIDEDCCAfigAwIBAgIINIibaGLpphAwDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxODE0NTMyM1oX%0ADTI5MDkxNTE0NTMyM1owJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy%0Ab290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4Mlg6s%2FpFdmH%0A8IYFhZSUhixiD26D%2FvzHzjXwmxZY9U9zh8hJXyqnawV8wAqflfRj6o%2FwgaIM6Gyj%0AxDS8t%2BHQXzqEwbREl05aEj6Z7B7Din2rXE7kHV%2FA9N%2FvF4BK1Xe%2FGUmic4N7CHNy%0AJ3fdAnDN7LAH2hl8KDyn%2BjSmZBFYIPYz5GQp8K9gzAthRmtjccN1wTGOL6AAGIrD%0AJdbdE0WzLXz3%2Ftzvm%2FWNGppLyQFzAizh5QsQlocIJcTEOolCedGrX2ghH4Wi0tlI%0AGzJ2LxcD%2BKM%2BIkxwoyhxLIo1fln7CuJ6Ajg5u3HDf2UXP2IVMmXv1jRSEAf%2FcH7p%0AKALMYLHfHwIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%0A%2FzAdBgNVHQ4EFgQUQadPsyYB4H7F3iNHX0XP3rVm3eUwDQYJKoZIhvcNAQELBQAD%0AggEBAIHDE6DK%2FqL3iFWHMYk61wVn2dUbwYF4vsfqm7rhZ9MmczTgxzMurn3U0rJw%0Auvw2xoMWKXpvfuVL2Ipljhc3sScLnZ7FMtPQ1MTScHwXCA%2FEAD3O0dKCDy4lec%2FE%0AFRBWvf3UFGLFycJNWKgdbd9vq4AiKpfLvoKtYSjcZeoY%2F8TuCzWQeVcegH8U%2BpkT%0AoNrG4tID%2BJL7dlEGo0PyAabqCvkV4HrkKsMHdpyjZy7zC4RRKx%2BO22AJuiTc11L9%0AR1wTlphO2B6hXM754PKLNYc2c%2BsaMw9nntapBFs%2BhhFD2talM5jXyrrB%2Foh3CvZk%0AfjVySk%2BTAbxG66FxQLqdW2dNf%2FU%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDMDCCAhigAwIBAgIIRApfIffG4fcwDQYJKoZIhvcNAQELBQAwNjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSAwHgYDVQQDExdhZG1pbi1rdWJlY29uZmlnLXNpZ25lcjAe%0AFw0xOTA5MTgxNDUzMjJaFw0yOTA5MTUxNDUzMjJaMDYxEjAQBgNVBAsTCW9wZW5z%0AaGlmdDEgMB4GA1UEAxMXYWRtaW4ta3ViZWNvbmZpZy1zaWduZXIwggEiMA0GCSqG%0ASIb3DQEBAQUAA4IBDwAwggEKAoIBAQDWAhl7ECtHpFHKKRhOez8ydzLTeUiG86kb%0An%2BiSeK7MeRHxiYIokPVkGrS3OLD1yxXQa5R5BnJxR9yzDmJK4BEQKyTFQzTd6xbZ%0ABrXAjy6MiUTfhM0Ke0bA71oIM65%2F6766csUAbebeceejkSR9u4%2BifmxltTbBbKTB%0AezYspx%2FXU1%2FhO9tH1wXSwdO3qi%2F12TYIzlw3a%2BrgdfJSKq0MoNEXfz75M2gNDWVA%0AQY6ssLu7SO4G3RbhsuR%2BQFsPd7ZxdiYijbuNV8XA9O6UbteJmQDo%2FvEyi9xafCgr%0A5pJn0g3RPqPsWowQrNh6b8MJWNC%2F2syqK0tf6H1SRKH394rjtUzrAgMBAAGjQjBA%0AMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMBAf8EBTADAQH%2FMB0GA1UdDgQWBBRgqIAg%0ADdPtxXzGkslXRzTZdVyeWzANBgkqhkiG9w0BAQsFAAOCAQEArqflTKTwAMjqlnR%2B%0AESEHb3HQvNVggcvvOAbYzw2dThLM0OtyxdcEd5b2ZOYxbKp1Zh5VjoIIzenq5gtj%0A1iogAA1ZOjYabNeOUjovlIcou2%2F8bpRH1zCO%2FN4MrkPjdXRBvs65uFdZ8AlOVYGc%0ApGI4uoeJFbZNP2t9DbqpsWstYW5j8Rz2aa2M2Jujvz0Bt7wln2bCr5BmUiL5cLS%2F%0Are7AA97vK7XWQYxeNKh%2BgVT5Of3vcC%2BPehzTrPt%2FMTKAJK6YTuFdhsYTRr6lmor7%0Ay6tyRd64FIAYFXGxwwUwcshuu%2BnAE1XvIwSyN0KXmi0kAXLj7EXCVWvgVnhlltZn%0A1jSW7w%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDHjCCAgagAwIBAgIIFZCGpKYvDSMwDQYJKoZIhvcNAQELBQAwLTESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRcwFQYDVQQDEw5rdWJlbGV0LXNpZ25lcjAeFw0xOTA5MTgx%0ANDUzMjdaFw0xOTA5MTkxNDUzMjdaMC0xEjAQBgNVBAsTCW9wZW5zaGlmdDEXMBUG%0AA1UEAxMOa3ViZWxldC1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEK%0AAoIBAQDNafBP4Bz3dryVxvRarO%2FIaVwc%2BahbUrqLVciK1aYULuCcUllicR%2FCGgvg%0AMTRBrvYZCghk8%2FOLLmDC4%2BQzYUlwh4orbEfHno8ssTEciNxSI1phOrtibM9vR6hh%0APBY%2BifqThn2AkIy%2FT3ZOrXqCZzEaSgWBtjlaYdE2nV4SM0RLq7iu93WxNdFvzBYh%0A6n3%2BWIn5M0IjYacJzHmCN5FP5l5TOEU0hPWi2xOCzjJpTW%2FWPq6ZqaubwuLtlK82%0AY%2FaO5ABn1wjjvEfXUCz6vzmx31ST1cmhNdJD0zZMsdOpGfGxULW%2BNCs8KzcYo%2BdC%0A9LpC3SLGHegPICAifu5gh8B1EsSPAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAP%0ABgNVHRMBAf8EBTADAQH%2FMB0GA1UdDgQWBBTYVxXl1CAOAItVVeff%2F%2F4DUAPXtDAN%0ABgkqhkiG9w0BAQsFAAOCAQEAWuwz%2Fx%2BguIS8IlavJKjNQ7zL849eRPaf%2BXF7Ie0h%0AdYi8wRW9WxRPRnt3%2BqHY%2BQAOz2eaF%2Bq7JchiMRhJb9%2F704fZG%2FsQZ6xb7f1l81vz%0Al1zVf9XAFNeE%2F02oPWj5eDSxEvjcim1FkQpaQ7e3zQ15hwWeGKkQIP91CpXrEsfy%0AxCw%2BZygxIt7lankDV%2BPTG%2BUqBqkKNfaSLUvJpoa%2BdNZFX5dKCMEBrERm4CAni%2Fyj%0AbNs5ZBSliDrJVc1rg7YEjVpID%2BH6AQi86%2Fw2xwH0kEhG%2B%2FNbE4a6FT9RBXTH2lqY%0AizF%2FFwcDAfW2gchAQ3Wg8p53sb2nNXrCWAdiTyCpW%2FpF0w%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDNDCCAhygAwIBAgIIVShd3S3Ot9gwDQYJKoZIhvcNAQELBQAwODESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSIwIAYDVQQDExlrdWJlLWNvbnRyb2wtcGxhbmUtc2lnbmVy%0AMB4XDTE5MDkxODE0NTMyN1oXDTIwMDkxNzE0NTMyN1owODESMBAGA1UECxMJb3Bl%0AbnNoaWZ0MSIwIAYDVQQDExlrdWJlLWNvbnRyb2wtcGxhbmUtc2lnbmVyMIIBIjAN%0ABgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA41IIjDIHyMPhE8cNCPtM58o5eatd%0A4wU7Opd54X4u8hhHvcJVhUR7Oh3L8AtFGp%2FenemVSF667ncfUqohKv0nFjbgIWEx%0AwjXuTz59xLu7dM93PoQqpjJ8C9PURXUD9KICS03pgDbEPLgzO4uZNpQ0XsYQzMk5%0A4RtiG9YJwj1QxMc2DCkuJePPqzaOPM6zl2Ju1VIIhtHz71n4apq5rsOgRDEa81xz%0AUCd0bXhsFFTXyIs6Pjzp31LMeleIaxolTkh8yByHJHYdn9LAr7Oplw00%2F391gfqz%0AZ4bq4hss2LR6fN1CUBmhGYvkKpD5p0wUVw%2BxLAhDPjqex7hnO39PIXaQbwIDAQAB%0Ao0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%2FzAdBgNVHQ4EFgQU%0ATmrzA5XUbU2Nvw%2FoHEhyb5lg6jgwDQYJKoZIhvcNAQELBQADggEBAKPtsGtoAK5Y%0AxhwTRS7oGpNtK5c5jR8AKzLemuY5BGVeK%2BzQu4KwTPHc4uyUf7V7vDTOq9CuLeej%0AAjJtCazB2uqG7U6M7H8AejL0ATm4CbFAQswfOcmNFDcz9QwYrBCPXQ5oFvBu%2Fics%0AqvnSnx%2BzYXcJLImpdVR%2BulBsxAuXa3fcct7IBC5Ysjeiq39txfbXnry9O6c7Lcy0%0AO%2FMm%2FWqC5UczDEKW9ZwljNi%2FofGLxmuFskpnD2CJTe8%2BdelGPNFqqFvhP3wzj429%0A%2Bw%2F8jCDxDEZokiW31AA4TVO9mf9YxWuWGOSQBsdJdx6B%2Fb9%2F0bB0UL1RYtiSYNMj%0A2C%2FkUvMUpB0%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDQjCCAiqgAwIBAgIIP4nZ4FYJQ0gwDQYJKoZIhvcNAQELBQAwPzESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSkwJwYDVQQDEyBrdWJlLWFwaXNlcnZlci10by1rdWJlbGV0%0ALXNpZ25lcjAeFw0xOTA5MTgxNDUzMjdaFw0yMDA5MTcxNDUzMjdaMD8xEjAQBgNV%0ABAsTCW9wZW5zaGlmdDEpMCcGA1UEAxMga3ViZS1hcGlzZXJ2ZXItdG8ta3ViZWxl%0AdC1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCe2dZh2d66%0AIDK6dFpY9bSHVVUkKX6AAxlXakPs1cr9pKS7A9j%2FM980NhOwY6oYQqNPxHRQ4FCZ%0AHADXTXscRHf1B7aGe5j2QvTsx2F%2F8enK%2BVwTK3Z6tcZdhajPaNEFle6xgNS%2B%2F8QZ%0AY4Y8KgWA817vH8RGjgaFcEWfwX%2BzQ0ULgqO6MOCbF0MStKRzPTBOF10HOqBgl9Qt%0AaXsM47lMDIKaDP5L1rOq9xy7nc50wBkRMkR9SSo4FE5oynZZe%2FKjIMIGQxzcehd3%0AZvtI8n9iPvCkBYvTRpXc1DYrubffUJbEFOGyw7Nns1yRbO4RszDl2IllPGbThBq1%0AbDO%2BXl2XgxpfAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMBAf8EBTAD%0AAQH%2FMB0GA1UdDgQWBBTj9aK1YBedBuFwrD221rZdXnFUIjANBgkqhkiG9w0BAQsF%0AAAOCAQEAnrD5EMXuJWU%2F8j9c5M0%2FN2e69xZ9EDfKvg%2BDzeg2wJmefS%2BgWIUuBbDm%0AlKeidso8Kc0mPv9hTzx1uKipR0Zt1HfeYI8v3HJy0O79owKAHesQUxdGMzOF2L%2BD%0AbSEbhcajvAX7hBKRvj1dHLKeHaudTo8r%2FCxKfvXjh5FECPo5yXYWoodLL7ktYfLZ%0APRNiEkmp0VzhNI6ibccbe6f78eIwr6EJQcpOlFAmG4iPsqEgMy3JvTmFFgzVDSPd%0ANLQp9Hmj4%2B552WD4%2FVX44Q%2BhHZO%2BJ%2B9Ti5QbeY%2BIwGrS36CQDHiTRnLAIlyX%2Bd2B%0AefJuJfR6iCBudlGGJtIA8AORn7ZoPw%3D%3D%0A-----END%20CERTIFICATE-----%0A-----BEGIN%20CERTIFICATE-----%0AMIIDSDCCAjCgAwIBAgIIWv2MTEp15xEwDQYJKoZIhvcNAQELBQAwQjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MSwwKgYDVQQDEyNrdWJlbGV0LWJvb3RzdHJhcC1rdWJlY29u%0AZmlnLXNpZ25lcjAeFw0xOTA5MTgxNDUzMjNaFw0yOTA5MTUxNDUzMjNaMEIxEjAQ%0ABgNVBAsTCW9wZW5zaGlmdDEsMCoGA1UEAxMja3ViZWxldC1ib290c3RyYXAta3Vi%0AZWNvbmZpZy1zaWduZXIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDM%0ARLgkYUNWZgrvqPRBwdsqGFBbv1CkNLpuGtQt%2FJIh%2Bf5Ha%2F9fhsbFSfjs2T%2FkBiwH%0AEEqjaiwhIcN%2BsxvubKJoJ%2B9zGyB%2F0hi7eCfhg53Tgx7zAV4SxIA%2Fb2ycoirBk%2FTU%0Adwt4mkZ%2FR9Cgwqcwq9FL2t9eDr5DCOgAkARrBNGxonWf96rHxW%2BvurHNC2yw3kFX%0Au6JoHtkIzB10Ob4cC3lO9b5VIatdcYIKQw1LOZwawU%2Ffo9%2F2HGG10J%2BiTRl2Lhoa%0A9V%2Fvqm9xY1vNM3t8%2Fsl2ABK%2FTYbzcTgqWRJoEVFFaUhqWu%2F%2Fg3miV4deGnlRBEXP%0AGOJ7mzD1ZmhNnZA1PetZAgMBAAGjQjBAMA4GA1UdDwEB%2FwQEAwICpDAPBgNVHRMB%0AAf8EBTADAQH%2FMB0GA1UdDgQWBBSj0TLepVBgOsp%2BTw2LzJsGB7iY0jANBgkqhkiG%0A9w0BAQsFAAOCAQEAJ3kANn3dkCmicvEjK%2FwhwT6eeI6IK2CSJy3pBDw9pn0Xmrt1%0AT9Hpj0NQvPVEq%2Bdw0utsXsMU6NmyCJ3n7PowQvXhbwOG5n4ATNX19Sih4dFihafa%0ATT0s0gMO4GIGFmEr8ew0kmA85ddrFtHtuMKTpk6IzhaLzx5Cu3BWxqCf1dxHEJHD%0A%2BOqSNJpERbmwPlDBEkqlzXc359U8ZTeoBE%2Bu8h9HmKB5MOlNd9P8TyryPXrQCdhZ%0AthdvOZcR9P1Ovw4YXs6pX4R5CpYi3Q%2FuvT77Wq3U7Ma%2FsklYnDSRshAeSj1FWgx1%0Ai4bO7%2F4OJ5%2B7enK3OINB%2B5%2FAKEMUgj6DecHi2A%3D%3D%0A-----END%20CERTIFICATE-----%0A"},"mode":420},{"path":"/etc/kubernetes/kubeconfig","contents":{"source":"data:,clusters%3A%0A-%20cluster%3A%0A%20%20%20%20certificate-authority-data%3A%20LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURNakNDQWhxZ0F3SUJBZ0lJWXlFcGEvRi9vcnd3RFFZSktvWklodmNOQVFFTEJRQXdOekVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1TRXdId1lEVlFRREV4aHJkV0psTFdGd2FYTmxjblpsY2kxc1lpMXphV2R1WlhJdwpIaGNOTVRrd09URTRNVFExTXpJeVdoY05Namt3T1RFMU1UUTFNekl5V2pBM01SSXdFQVlEVlFRTEV3bHZjR1Z1CmMyaHBablF4SVRBZkJnTlZCQU1UR0d0MVltVXRZWEJwYzJWeWRtVnlMV3hpTFhOcFoyNWxjakNDQVNJd0RRWUoKS29aSWh2Y05BUUVCQlFBRGdnRVBBRENDQVFvQ2dnRUJBS1FiN0FOUTBWN3ZvL3VOYVcwTVJjUjFZSXh5WWlURApNUmN1UlJyQ2VMaEFLQkVIOGc0b01pWGswL1lYc0hEQWlZdGtMNHpsZU91RlJZK1lYazJQOWl5bk8rSTFPQ0svCkVKaTY5QVdVZnkxSk5oNDFpU1ZBTnNaeUZHUW5UVDN3V3NrMjhFTitGQ2FkTVBWTDFmZGNnUUIwWjhjRlVpOFgKN3ZRR2NSOWtsckpUeUpoem5LQkFxbE5HOGQ1R2pFS3JPRE5PVFlLVklFYXdNb2dwV3pNVm41ekRiV0FEUUJ0KwpCYnBCVFFvTVQ2dGpldkE0OVFoajVDa21RT1N1WGZicWVTNkJqa05rUGNCQmVIdDlFTWpMQWZDYnNvZ0VkajRMCnZsMUxwMHJBZ2VYbzNDY0ZONkdmdUwwMUtISUEzb2l0aTNEeWpwandIRjVNS3g4TW9zbVVEdXNDQXdFQUFhTkMKTUVBd0RnWURWUjBQQVFIL0JBUURBZ0trTUE4R0ExVWRFd0VCL3dRRk1BTUJBZjh3SFFZRFZSME9CQllFRkJuQwovd0tJeDFZRFhVSXdLUmlaSGxPTGZjN3lNQTBHQ1NxR1NJYjNEUUVCQ3dVQUE0SUJBUUNWMkRqWXdPdjB6OG9aClp0aWZodWhkWVhBeUdKNWNxSlNYU2J6Y1lkazBPYUVMNW1CanlLemNRLzdEWUh0L1hSWDZUelZtQXIyMWtMOHoKN3YxeXVaLzhOZitualQ0TXhJSHZkUmJSOVpQY2RIWFJYeFNBUkE4YXZ6Y0RoUjhUMkM0TW82MmcyZllBemt3aQpDZzhaQ3JKNndLOEdJdG92dEdQSm5IaEJpY3g0ak5oTXFtNGJpV3VlS0dVeHZ2eThNNUVvOUJYZnliZFJ4RHVHCjVnc2kyVWFvcHN4bVJ1dUNBd2tPNFluVDAyc3pzbTA3cEdUeGtUcjU0QzhwcGh6OGd6VUEreTNZanNRcFltOVEKZk0rNVcwbkRxU2xFY0J3cUpleFZZanNvTWtlUUt0aExINmxybjh0UXRabzJGWHExQUhrT2dNaGhJV2p1aUdXWQpkdHN3c2FjZgotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tQkVHSU4gQ0VSVElGSUNBVEUtLS0tLQpNSUlEUURDQ0FpaWdBd0lCQWdJSVZFRmxDTDNFRUdjd0RRWUpLb1pJaHZjTkFRRUxCUUF3UGpFU01CQUdBMVVFCkN4TUpiM0JsYm5Ob2FXWjBNU2d3SmdZRFZRUURFeDlyZFdKbExXRndhWE5sY25abGNpMXNiMk5oYkdodmMzUXQKYzJsbmJtVnlNQjRYRFRFNU1Ea3hPREUwTlRNeU1sb1hEVEk1TURreE5URTBOVE15TWxvd1BqRVNNQkFHQTFVRQpDeE1KYjNCbGJuTm9hV1owTVNnd0pnWURWUVFERXg5cmRXSmxMV0Z3YVhObGNuWmxjaTFzYjJOaGJHaHZjM1F0CmMybG5ibVZ5TUlJQklqQU5CZ2txaGtpRzl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUE1c3gyZFFqeStBQm0KWTZscDlrK1lnQ2hrL1ZQcTNxY0lsOFkwY3JwUXVCb0pEdmtRbXloU1FFZXpCWG1qWjh2RC9NK0I1Q1JwbTF6TgpDVGtwMHliU1M3dDliNFJlbGpVbldhMTlMRnl4d3VXWE1rKzNidHBEN1VFbnc1RHkyV0hsOHdUTWJtbDRZSmhDCmhuTlpQN25nZmdwdUpHQmJncDV4M2JQQ3plSklDNktITklLWXM1QUdONGx3ZkJCV2lWa3dJemh1QlZ4QWxmbHAKakhSTmZtVXQrQnh6N1poa3pjYldzZTF3SXhJMGJKYzRsWVh1RThBdzlzUmJrcEZiS001Z21CVktKVHJoMk1RWgowMk5ieE16eWlvTjR2eEExcHltd3h1ME82ODV4VWY5NmlZeExoTmFoRHM3MC9xNzdPVmdPdmVIcnBmOGVWb2ROCnhVY2M1ZWorZVFJREFRQUJvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvQkFVd0F3RUIKL3pBZEJnTlZIUTRFRmdRVWlrd2NsRjdVUjhOK0crTTlGN0x1UElEYnFnRXdEUVlKS29aSWh2Y05BUUVMQlFBRApnZ0VCQUJzOWpJSVpHcjNERGJ2cUJqTWdTdEZhaDZuSjg0a2dSZldlS01nVTBrTmlGaXlYMEhVa3RmbEo2NGR4CitmVUJ1TFV4VHhGK3NLNEhQK1FZMXpTcDVnUHI1Nm95QklmUWdORm1SNDlJaktpM0xjaFJLZ3Vvc1hBOWdCMzgKdDQ1RitBNk5tVDI4TzNsMnoyRDFVVy8ySGVyZ29jNXQ3Mzk2QTBUQUFSRlVyekZjSTdibU1RU0s3dFhSSDNPdApWK2JLRElNeTJ5N25wcUtQMU0zNkJjcDYvUTZCZGdMZ2UyaDlmTmkxcVNZdmphNkcvT0hvcFRlNVpmZXQvNzh0CkRIOTBqS2wxOVlHODlZa3ZiWWtFMHUvcER2dUxXblRSVWZRRkZzL3FRUys3VEJHT05lRUczN29RMDJhbml5d3EKSGlXWXNCUXdDVVBMdG4yWHFDaldkNzBnMjFjPQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tQkVHSU4gQ0VSVElGSUNBVEUtLS0tLQpNSUlEVERDQ0FqU2dBd0lCQWdJSVF1dnFXcDBJTGpFd0RRWUpLb1pJaHZjTkFRRUxCUUF3UkRFU01CQUdBMVVFCkN4TUpiM0JsYm5Ob2FXWjBNUzR3TEFZRFZRUURFeVZyZFdKbExXRndhWE5sY25abGNpMXpaWEoyYVdObExXNWwKZEhkdmNtc3RjMmxuYm1WeU1CNFhEVEU1TURreE9ERTBOVE15TWxvWERUSTVNRGt4TlRFME5UTXlNbG93UkRFUwpNQkFHQTFVRUN4TUpiM0JsYm5Ob2FXWjBNUzR3TEFZRFZRUURFeVZyZFdKbExXRndhWE5sY25abGNpMXpaWEoyCmFXTmxMVzVsZEhkdmNtc3RjMmxuYm1WeU1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0MKQVFFQXRNNVdhRkVTNmx2U1N6ekdadHNGV1hrN09Xb3YzQXNjRDBOU3owMGVyZSt6cmx4c0dDbDNzQXFlb0paMQphZ01yL3dIOTI4Mk1taytCdmc1THZRRXI4MlNDdC9XOVo4Y1VCWm1xOGpXdzRiNi93MTNkSlNIQTRLb2xaNG5nCkhIa3l6NUhpL0o5L0ZveHhqMnJhSzBjYlVmVmhpdmNLK3MrZy83djZVWUs0ZWFEaWcySVBsR01wMTV4QUN5WEkKUEp2eTRhU09nNjVaT2FLS0ZwaHRGcjNKVFpRdzBZMlB3TVBuaXZxWXNjcWloTVNqMlRyUGpEeXRCS2YwQ0kraApqTUJEbURoMmRRZWZMcDNuV3E2Q2dBQzNvNFNjTmhHbkdLeEtiWmZCd2YrSW44N3J5UEhzL2JubGZXbGVLc0lPCmpXd0pqYThjczF4QWlVYTNCaHJnQTVEQkN3SURBUUFCbzBJd1FEQU9CZ05WSFE4QkFmOEVCQU1DQXFRd0R3WUQKVlIwVEFRSC9CQVV3QXdFQi96QWRCZ05WSFE0RUZnUVV5UFhCQVZldC9hMDRBUUlpQUEvT3M1a3cvNGN3RFFZSgpLb1pJaHZjTkFRRUxCUUFEZ2dFQkFHa1E2dmtKdlNncVQzdGZqdjlGR3JHeUZDaWFheGtJaU5kTm5GZDhsWGgyCmUxbGhqNXRUekpqbnhWUGNwQm5yc0tUK1YzTTEyYkY0U2VIQlV0SE9ENHJkbk5aMXBzVEE1b1pHb2NuTllHbkMKSFU1OEp2ZWZMcjc1RlAzbnpEdE44T0QvYlpBdE9UbktmSnZNWFNTcThVZ055bFhPQTIyNEg0VmtnV0E4N3J4NQpIQzdMSUhSTVllK01RbmhIOVNKby8xUXl0SE12S3NKaHBuRDRYc0I0SkE5TmhNTzFzWC9pT3VXMEtJbHNUdTJWCkpnNVZOZWp3YllXR2hpaGhGVkNENjFsUy9uUFRnY3RGSmNmdzhudTF3QWhrR2NFQ0xuUENFd1VDSEVkUXlNODMKWjFaclhZeHdnMHVwektubnMzTTlrZ2tFRC9iMmZIUjJNV1NWS2w1WEFnOD0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQotLS0tLUJFR0lOIENFUlRJRklDQVRFLS0tLS0KTUlJQzdUQ0NBZFdnQXdJQkFnSUJBVEFOQmdrcWhraUc5dzBCQVFzRkFEQW1NU1F3SWdZRFZRUUREQnRwYm1keQpaWE56TFc5d1pYSmhkRzl5UURFMU5qZzRNVGsyTlRBd0hoY05NVGt3T1RFNE1UVXhOREE1V2hjTk1qRXdPVEUzCk1UVXhOREV3V2pBbU1TUXdJZ1lEVlFRRERCdHBibWR5WlhOekxXOXdaWEpoZEc5eVFERTFOamc0TVRrMk5UQXcKZ2dFaU1BMEdDU3FHU0liM0RRRUJBUVVBQTRJQkR3QXdnZ0VLQW9JQkFRRFgweEs0ZU5tVzBPYVR2NXlkL2hiWQpxYWliQml0SlFDM3B6SlpVTWdCR0V0dGgrZ1FIT09aK2d0OUxXSUFCU1pJdWFaN25yMjZleXpZeDFiRTJ6ZGk5CjRyZis4anFNS3ViMHhLcCtJLzA3Uy9VdzBDVU5aVDZxeVFvdi96N3EwQVZqNUJiRU9kOUZzUU42TU1ReTVzMUUKTjU0SXBsM3l5cVNUbm9ZcnhDV3RNTEVmeEpacXZhNnBHRmdGRHA0dUQwMldNR01LZjNtRnZicitFUDc4SXlBZQorcnRvMHNvd2xsWVZYK050QnlHU29pSzRqa3MwZ29qaXFMaEJQUHNQT1psbGQyV3RrTmRFUDUyL0Q3bUQ1K29DCnEyRG9DV2ZMSlNkSUZvUjdGRDdJb09Cd1VqMUc2a3NOOG9HaDVwZ0FRL2tVT2pEd0hkK1hqTW1KMXNHcEQrVlYKQWdNQkFBR2pKakFrTUE0R0ExVWREd0VCL3dRRUF3SUNwREFTQmdOVkhSTUJBZjhFQ0RBR0FRSC9BZ0VBTUEwRwpDU3FHU0liM0RRRUJDd1VBQTRJQkFRQmE5R0pxSHBKRzMyekdITm1DS3M4Q3VReGEwendjZHVvdnMxMkFsYWZlCmptQ21FbUx6MnVkUk1MMjByU2pEQ2Z4a1U2bTRzbnJINVdMYlpMazk0R1VjZ2lTVG1MeHMvSFZzbjJrcVhkTEMKLzRaQjcyUk0veURBZGRYaFFGRGlqbER1Yi9CT2o1OUt5UGNUNnA1SHNQMnpkRi9Yd2ViYWNVMlpUM1c5S29mZQpRcmttMFBwb2I4eERZYndTeGh3SUVxN1ZsZ3Vaalo2VEg0RTJ1dmcvT1VJZ2Q4SE9ndFhrazd4dGdPRTdVTUJqCnN2QW03VGRuWi9YLzd4YlU0cHRyK01jWk1BaXNjVk1TMXlmaUpsM3VVZ01abFNTaWNiVkF0Y0NmbDkwQU5VQXEKdzkyZWxFNHBwcTIwWjk4WlhEcXRVNWpUcTVZemlwZUMvMjhTRnNHNGdjakUKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo%3D%0A%20%20%20%20server%3A%20https%3A%2F%2Fapi-int.ssoto-dev.devcluster.openshift.com%3A6443%0A%20%20name%3A%20local%0Acontexts%3A%0A-%20context%3A%0A%20%20%20%20cluster%3A%20local%0A%20%20%20%20user%3A%20kubelet%0A%20%20name%3A%20kubelet%0Acurrent-context%3A%20kubelet%0Apreferences%3A%20%7B%7D%0Ausers%3A%0A-%20name%3A%20kubelet%0A%20%20user%3A%0A%20%20%20%20token%3A%20eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJvcGVuc2hpZnQtbWFjaGluZS1jb25maWctb3BlcmF0b3IiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoibm9kZS1ib290c3RyYXBwZXItdG9rZW4iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoibm9kZS1ib290c3RyYXBwZXIiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiI4MzIzMDk4Mi1kYTI2LTExZTktYjNhZi0wYTI4YTRlMmU2OGUiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6b3BlbnNoaWZ0LW1hY2hpbmUtY29uZmlnLW9wZXJhdG9yOm5vZGUtYm9vdHN0cmFwcGVyIn0.tveE4Zf_VIqBocwKrQlX2ZWLZ0ZXxUzgF12XxI2NulvGf90M55kYxQE419kuum5J961iq02CdH0qrM_dfI6USVJ40wWfCaZG94bWEWO2fA9tXKAKBgi-9FKdrTkmDwGtCdL4KXP7kCIPnbwBiTEg7-_0P5ok1rTKSoMPP7auCaqGafZwbckgSWRe5K3UBaEk_MQeqfqIuVC0dK6c8ra5dj3WZWLYyEDxk8cAI_YBxXQ438N-G2I-Y74y_ha-pwlYcjP40Yp4ldM7GlBnGdzzD0z78aqq54KVP2HTvMx8Qte5hPi7UqsV3Xigk4YwQpxwV4I_GdalNHzTJZxRjuA6-A%0A"},"mode":420}]},"systemd":{"units":[{"contents":"[Unit]\nDescription=Kubernetes Kubelet\nWants=rpc-statd.service crio.service\nAfter=crio.service\n\n[Service]\nType=notify\nExecStartPre=/bin/mkdir --parents /etc/kubernetes/manifests\nExecStartPre=/bin/rm -f /var/lib/kubelet/cpu_manager_state\nEnvironmentFile=/etc/os-release\nEnvironmentFile=-/etc/kubernetes/kubelet-workaround\nEnvironmentFile=-/etc/kubernetes/kubelet-env\n\nExecStart=/usr/bin/hyperkube \\\n    kubelet \\\n      --config=/etc/kubernetes/kubelet.conf \\\n      --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \\\n      --kubeconfig=/var/lib/kubelet/kubeconfig \\\n      --container-runtime=remote \\\n      --container-runtime-endpoint=/var/run/crio/crio.sock \\\n      --node-labels=node-role.kubernetes.io/worker,node.openshift.io/os_id=${ID} \\\n      --minimum-container-ttl-duration=6m0s \\\n      --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \\\n      --cloud-provider=aws \\\n       \\\n      --v=3\n\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n","enabled":true,"name":"kubelet.service"}]}}`

func init() {
	pflag.StringVar(&ignitionFilePath, "ignition-file", "C:\\Windows\\Temp\\worker.ign", "ign file location")
	pflag.StringVar(&kubeletPath, "kubelet-path", "C:\\Windows\\Temp\\kubelet.exe", "kubelet location")
	pflag.StringVar(&installDir, "install-dir", "C:\\k", "Installation directory")
	pflag.StringVar(&platformType, "platform-type", "", "platform type")
}

// TestBootstrapper tests that the bootstrapper was able to start the required services
// TODO: Consider adding functionality to this test to check if the underlying processes are running properly,
//  	 otherwise keep that functionality contained within other future tests
func TestBootstrapper(t *testing.T) {
	var kubeletRunningBeforeTest bool
	// We make a best effort to ensure that there is a ignition file on the node.
	// TODO: Consider doing the same with kubelet. We can either provide our own or download it from the internet
	// 		 if we choose to download it, we will have to compare expected vs actual SHA hashes for security reasons
	ensureIgnitionFileExists(t, ignitionFilePath)

	// If the kubelet is not running yet, we can run disruptive tests
	if svcExists(t, bootstrapper.KubeletServiceName) && svcRunning(t, bootstrapper.KubeletServiceName) {
		kubeletRunningBeforeTest = true
	}
	if !kubeletRunningBeforeTest {
		// Remove the kubelet logfile, so that when we parse it, we are looking at the current run only
		removeFileIfExists(t, kubeletLogPath)
	}

	t.Run("Configure CNI without kubelet service present", testConfigureCNIWithoutKubeletSvc)

	t.Run("Uninstall kubelet without kubelet service present", testUninstallWithoutKubeletSvc)

	// Run the bootstrapper, which will start the kubelet service
	wmcb, err := bootstrapper.NewWinNodeBootstrapper(installDir, ignitionFilePath, kubeletPath, "", "", "", "", platformType)
	require.NoErrorf(t, err, "Could not create WinNodeBootstrapper: %s", err)
	err = wmcb.InitializeKubelet()
	assert.NoErrorf(t, err, "Could not run bootstrapper: %s", err)

	t.Run("Kubelet Windows service starts", func(t *testing.T) {
		// Wait for the service to start
		time.Sleep(2 * time.Second)
		assert.Truef(t, svcRunning(t, bootstrapper.KubeletServiceName), "The kubelet service is not running")
	})

	t.Run("Kubelet enters running state", func(t *testing.T) {
		if kubeletRunningBeforeTest {
			t.Skip("Skipping as kubelet was already running before the test")
		}
		// Wait for kubelet log to be populated
		time.Sleep(waitTimeKubeletLog)
		assert.True(t, isKubeletRunning(t, kubeletLogPath))
	})

	t.Run("Update already running kubelet service", func(t *testing.T) {
		err := wmcb.InitializeKubelet()
		assert.NoError(t, err, "unable to update kubelet service")
		err = wmcb.Disconnect()
		assert.NoErrorf(t, err, "Could not disconnect from windows svc API: %s", err)

		err = wait.Poll(pollIntervalKubeletLog, waitTimeKubeletLog, func() (done bool, err error) {
			return isKubeletRunning(t, kubeletLogPath), nil
		})
		assert.NoError(t, err)
	})

	// Kubelet arguments with paths that are set by bootstrapper
	// Does not include node-labels and container-image since their paths do not depend on underlying OS
	checkPathsFor := []string{"--bootstrap-kubeconfig", "--cloud-config", "--config", "--kubeconfig", "--log-file",
		"--cert-dir"}
	expectedDependencies := []string{"docker"}

	_, path, actualDependencies, err := getSvcInfo(bootstrapper.KubeletServiceName)
	require.NoError(t, err, "Could not get kubelet arguments")

	t.Run("Test the paths in Kubelet arguments", func(t *testing.T) {
		testPathInKubeletArgs(t, checkPathsFor, path)
	})
	t.Run("Test the config dependencies in Kubelet arguments", func(t *testing.T) {
		assert.ElementsMatch(t, expectedDependencies, actualDependencies)
	})
}

// ensureIgnitionFileExists will create a generic ignition file if one is not provided on the node
func ensureIgnitionFileExists(t *testing.T, path string) {
	if fp, err := os.Open(path); err == nil {
		fp.Close()
		return
	}
	fp, err := os.Create(path)
	defer fp.Close()
	require.NoError(t, err)
	_, err = fp.WriteString(defaultIgnitionFileContents)
	assert.NoError(t, err)
}

// svcRunning returns true if the service with the name svcName is running
func svcRunning(t *testing.T, svcName string) bool {
	state, _, _, err := getSvcInfo(svcName)
	assert.NoError(t, err)
	return svc.Running == state
}

// svcExists returns true with the service with the name svcName is installed, the state of the service does not matter
func svcExists(t *testing.T, svcName string) bool {
	svcMgr, err := mgr.Connect()
	require.NoError(t, err)
	defer svcMgr.Disconnect()
	mySvc, err := svcMgr.OpenService(svcName)
	if err != nil {
		return false
	} else {
		mySvc.Close()
		return true
	}
}

// getSvcInfo gets the current state, fully qualified path and config dependencies of the specified service.
// Requires administrator privileges
func getSvcInfo(svcName string) (svc.State, string, []string, error) {
	// State(0) is equivalent to "Stopped"
	state := svc.State(0)
	svcMgr, err := mgr.Connect()
	if err != nil {
		return state, "", nil, fmt.Errorf("could not connect to Windows SCM: %s", err)
	}
	defer svcMgr.Disconnect()
	mySvc, err := svcMgr.OpenService(svcName)
	if err != nil {
		// Could not find the service, so it was never created
		return state, "", nil, err
	}
	defer mySvc.Close()
	// Get state of Service
	status, err := mySvc.Query()
	if err != nil {
		return state, "", nil, err
	}
	// Get fully qualified path of Service
	config, err := mySvc.Config()
	if err != nil {
		return state, "", nil, err
	}
	if config.BinaryPathName != "" {
		return status.State, config.BinaryPathName, config.Dependencies, nil
	} else {
		return status.State, "", nil, fmt.Errorf("could not fetch %s path: %s", svcName, err)
	}
}

// removeFileIfExists removes the file given by 'path', and will not throw an error if it does not exist
func removeFileIfExists(t *testing.T, path string) {
	err := os.Remove(path)
	if err != nil {
		require.Truef(t, os.IsNotExist(err), "could not remove file %s: %s", path, err)
	}
}

// isKubeletRunning checks if the kubelet was able to start sucessfully
func isKubeletRunning(t *testing.T, logPath string) bool {
	buf, err := ioutil.ReadFile(logPath)
	assert.NoError(t, err)
	return strings.Contains(string(buf), "Started kubelet")
}

// testPathInKubeletArgs checks if the paths given as arguments to kubelet service are correct
// Only checks for paths that are dependent on the underlying OS
func testPathInKubeletArgs(t *testing.T, checkPathsFor []string, path string) {
	// Split the arguments from kubelet path
	kubeletArg := strings.Split(path, " ")
	for _, arg := range kubeletArg {
		// Split the key and value of arg
		argSplit := strings.SplitN(arg, "=", 2)
		// Ignore single valued arguments
		if len(argSplit) > 1 {
			for _, key := range checkPathsFor {
				if key == argSplit[0] {
					assert.Containsf(t, argSplit[1], string(os.PathSeparator), "Path not correctly set for %s", key)
				}
			}
		}
	}
}
