package cloud

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// getAWSMetadataHostname returns name of the AWS host from metadata service
func getAWSMetadataHostname() (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return "", fmt.Errorf("unable to load config: %w", err)
	}

	client := imds.NewFromConfig(cfg)

	// For compatibility with the AWS in-tree provider
	// Set node name to be instance name instead of the default FQDN hostname
	//
	// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html
	hostnameRes, err := client.GetMetadata(context.TODO(), &imds.GetMetadataInput{
		Path: "local-hostname",
	})
	if err != nil {
		return "", fmt.Errorf("unable to retrieve the hostname from the EC2 instance: %w", err)
	}

	defer hostnameRes.Content.Close()
	hostname, err := io.ReadAll(hostnameRes.Content)
	if err != nil {
		return "", fmt.Errorf("cannot read hostname from the EC2 instance: %w", err)
	}

	return string(hostname), nil
}
