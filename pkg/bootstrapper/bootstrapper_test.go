package bootstrapper

import (
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"testing"
)

// TestTranslateFile tests decoding and transforming ignition file sources
func TestTranslateFile(t *testing.T) {
	type args struct {
		input  string
		lambda translationFunc
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "No translation function",
			args: args{
				input:  "data:,-----BEGIN%20CERTIFICATE-----%0AMIIDEDCCAfigAwIBAgIIKH9ePWRYTs8wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX%0ADTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy%0Ab290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5%0A%2Bw0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5%0AEw3Ovuaz%2FKcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ%0Ad%2FuZnRIT7dsR%2BVdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM%0AkP%2Fy73wzKEMtWPjNA%2BJaJdNf4n7Mh57fwO9IMrmMQWZP7d%2B8kFMnfQygXPopqFQR%0ADhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E%2BwCZ7HQV8J5V6WlO8wR0R%0AC8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%0A%2FzAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N%2Bd%2BAxxOkwDQYJKoZIhvcNAQELBQAD%0AggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42%0A%2BSKzN63i%2FSreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7%2BfWZmD4VBrUqI%0AFbrOEMZbc9vPxvpbN%2FinzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn%2Fx%0AK58cZ7SBbK9oyk%2FCF2St%2F9TR7unuNFDq1TPsjSKxJMC%2FsTyEcW6ABCOjcqu94eWt%0AUHfH1Be25D8kcN0%2FtdrJt4NgawQINUr0QIkSsY%2B3hh8AUHSvyCbiiCrt%2Fn7jjF7G%0ArqLuyNO%2BhCh%2FZclPL%2BUiGJH1dlQ%3D%0A-----END%20CERTIFICATE-----",
				lambda: nil,
			},
			want: []byte(`-----BEGIN CERTIFICATE-----
MIIDEDCCAfigAwIBAgIIKH9ePWRYTs8wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE
CxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX
DTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy
b290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5
+w0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5
Ew3Ovuaz/KcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ
d/uZnRIT7dsR+VdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM
kP/y73wzKEMtWPjNA+JaJdNf4n7Mh57fwO9IMrmMQWZP7d+8kFMnfQygXPopqFQR
DhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E+wCZ7HQV8J5V6WlO8wR0R
C8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N+d+AxxOkwDQYJKoZIhvcNAQELBQAD
ggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42
+SKzN63i/SreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7+fWZmD4VBrUqI
FbrOEMZbc9vPxvpbN/inzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn/x
K58cZ7SBbK9oyk/CF2St/9TR7unuNFDq1TPsjSKxJMC/sTyEcW6ABCOjcqu94eWt
UHfH1Be25D8kcN0/tdrJt4NgawQINUr0QIkSsY+3hh8AUHSvyCbiiCrt/n7jjF7G
rqLuyNO+hCh/ZclPL+UiGJH1dlQ=
-----END CERTIFICATE-----`),
		},
		{
			name: "Using translation function",
			args: args{
				input: "data:,-----BEGIN%20CERTIFICATE-----%0AMIIDEDCCAfigAwIBAgIIKH9ePWRYTs9wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX%0ADTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy%0Ab290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5%0A%2Bw0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5%0AEw3Ovuaz%2FKcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ%0Ad%2FuZnRIT7dsR%2BVdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM%0AkP%2Fy73wzKEMtWPjNA%2BJaJdNf4n7Mh57fwO9IMrmMQWZP7d%2B8kFMnfQygXPopqFQR%0ADhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E%2BwCZ7HQV8J5V6WlO8wR0R%0AC8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%0A%2FzAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N%2Bd%2BAxxOkwDQYJKoZIhvcNAQELBQAD%0AggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42%0A%2BSKzN63i%2FSreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7%2BfWZmD4VBrUqI%0AFbrOEMZbc9vPxvpbN%2FinzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn%2Fx%0AK58cZ7SBbK9oyk%2FCF2St%2F9TR7unuNFDq1TPsjSKxJMC%2FsTyEcW6ABCOjcqu94eWt%0AUHfH1Be25D8kcN0%2FtdrJt4NgawQINUr0QIkSsY%2B3hh8AUHSvyCbiiCrt%2Fn7jjF7G%0ArqLuyNO%2BhCh%2FZclPL%2BUiGJH1dlQ%3D%0A-----END%20CERTIFICATE-----",
				lambda: func(bs *winNodeBootstrapper, in []byte) ([]byte, error) {
					return []byte(string(in) + "suffix"), nil
				},
			},
			want: []byte(`-----BEGIN CERTIFICATE-----
MIIDEDCCAfigAwIBAgIIKH9ePWRYTs9wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE
CxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX
DTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy
b290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5
+w0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5
Ew3Ovuaz/KcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ
d/uZnRIT7dsR+VdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM
kP/y73wzKEMtWPjNA+JaJdNf4n7Mh57fwO9IMrmMQWZP7d+8kFMnfQygXPopqFQR
DhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E+wCZ7HQV8J5V6WlO8wR0R
C8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N+d+AxxOkwDQYJKoZIhvcNAQELBQAD
ggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42
+SKzN63i/SreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7+fWZmD4VBrUqI
FbrOEMZbc9vPxvpbN/inzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn/x
K58cZ7SBbK9oyk/CF2St/9TR7unuNFDq1TPsjSKxJMC/sTyEcW6ABCOjcqu94eWt
UHfH1Be25D8kcN0/tdrJt4NgawQINUr0QIkSsY+3hh8AUHSvyCbiiCrt/n7jjF7G
rqLuyNO+hCh/ZclPL+UiGJH1dlQ=
-----END CERTIFICATE-----suffix`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := winNodeBootstrapper{installDir: filepath.Base("tmp")}
			got, err := bs.translateFile(tt.args.input, tt.args.lambda)
			assert.Nil(t, err)
			assert.Equalf(t, tt.want, got, "got = %v, want %v", string(got), string(tt.want))
		})
	}
}

// TestPrepKubeletConfForWindows tests that we are changing the kubelet configuration in a way that allows it to run on windows
func TestPrepKubeletConfForWindows(t *testing.T) {
	type args struct {
		in []byte
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "Base case",
			args: args{in: []byte(`{"kind":"KubeletConfiguration","apiVersion":"kubelet.config.k8s.io/v1beta1","staticPodPath":"/etc/kubernetes/manifests","syncFrequency":"0s","fileCheckFrequency":"0s","httpCheckFrequency":"0s","rotateCertificates":true,"serverTLSBootstrap":true,"authentication":{"x509":{"clientCAFile":"/etc/kubernetes/kubelet-ca.crt"},"webhook":{"cacheTTL":"0s"},"anonymous":{"enabled":false}},"authorization":{"webhook":{"cacheAuthorizedTTL":"0s","cacheUnauthorizedTTL":"0s"}},"clusterDomain":"cluster.local","clusterDNS":["172.30.0.10"],"streamingConnectionIdleTimeout":"0s","nodeStatusUpdateFrequency":"0s","nodeStatusReportFrequency":"0s","imageMinimumGCAge":"0s","volumeStatsAggPeriod":"0s","cgroupDriver":"systemd","cpuManagerReconcilePeriod":"0s","runtimeRequestTimeout":"10m0s","maxPods":250,"serializeImagePulls":false,"evictionPressureTransitionPeriod":"0s","featureGates":{"ExperimentalCriticalPodAnnotation":true,"LocalStorageCapacityIsolation":false,"RotateKubeletServerCertificate":true,"SupportPodPidsLimit":true},"containerLogMaxSize":"50Mi","systemReserved":{"cpu":"500m","memory":"500Mi"}}`)},
			want: []byte(`{"kind":"KubeletConfiguration","apiVersion":"kubelet.config.k8s.io/v1beta1","staticPodPath":"/etc/kubernetes/manifests","syncFrequency":"0s","fileCheckFrequency":"0s","httpCheckFrequency":"0s","rotateCertificates":true,"serverTLSBootstrap":true,"authentication":{"x509":{"clientCAFile":"C:\\k\\kubelet-ca.crt"},"webhook":{"cacheTTL":"0s"},"anonymous":{"enabled":false}},"authorization":{"webhook":{"cacheAuthorizedTTL":"0s","cacheUnauthorizedTTL":"0s"}},"clusterDomain":"cluster.local","clusterDNS":["172.30.0.10"],"streamingConnectionIdleTimeout":"0s","nodeStatusUpdateFrequency":"0s","nodeStatusReportFrequency":"0s","imageMinimumGCAge":"0s","volumeStatsAggPeriod":"0s","cgroupsPerQOS":false,"cgroupDriver":"cgroupfs","cpuManagerReconcilePeriod":"0s","runtimeRequestTimeout":"10m0s","maxPods":250,"serializeImagePulls":false,"evictionPressureTransitionPeriod":"0s","featureGates":{"ExperimentalCriticalPodAnnotation":true,"LocalStorageCapacityIsolation":false,"RotateKubeletServerCertificate":true,"SupportPodPidsLimit":true},"containerLogMaxSize":"50Mi","systemReserved":{"cpu":"500m","memory":"500Mi"},"enforceNodeAllocatable":[]}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := winNodeBootstrapper{installDir: `C:\k`}
			got, err := prepKubeletConfForWindows(&bs, tt.args.in)
			assert.Nil(t, err)
			assert.Equalf(t, tt.want, got, "got = %v, want %v", string(got), string(tt.want))
		})
	}
}
