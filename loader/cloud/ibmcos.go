// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build ibm

package cloud

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam"
	awssession "github.com/IBM/ibm-cos-sdk-go/aws/session"
	ibms3 "github.com/IBM/ibm-cos-sdk-go/service/s3"
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

// IBMCOSLoader loads configuration from IBM Cloud Object Storage.
type IBMCOSLoader struct {
	bucketName        string
	objectKey         string
	apiKey            string
	serviceInstanceID string
	endpointURL       string
	region            string
}

// IBMCOSOption configures an IBMCOSLoader.
type IBMCOSOption func(*IBMCOSLoader)

// WithIBMEndpoint sets a custom endpoint URL.
func WithIBMEndpoint(url string) IBMCOSOption {
	return func(l *IBMCOSLoader) { l.endpointURL = url }
}

// WithIBMRegion sets the IBM Cloud region.
func WithIBMRegion(region string) IBMCOSOption {
	return func(l *IBMCOSLoader) { l.region = region }
}

// NewIBMCOS creates a loader for bucket and key. Credentials are read from
// IBM_API_KEY and IBM_SERVICE_INSTANCE_ID. Region defaults to us-south and
// determines the endpoint unless WithIBMEndpoint overrides it. Construction
// performs no network request.
func NewIBMCOS(bucket, key string, opts ...IBMCOSOption) *IBMCOSLoader {
	l := &IBMCOSLoader{
		bucketName:        bucket,
		objectKey:         key,
		apiKey:            os.Getenv("IBM_API_KEY"),
		serviceInstanceID: os.Getenv("IBM_SERVICE_INSTANCE_ID"),
		region:            "us-south",
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.endpointURL == "" {
		l.endpointURL = fmt.Sprintf("https://s3.%s.cloud-object-storage.appdomain.cloud", l.region)
	}
	return l
}

// Source returns the identifier for this loader's configuration source.
func (l *IBMCOSLoader) Source() string {
	return fmt.Sprintf("ibmcos://%s/%s", l.bucketName, l.objectKey)
}

// Load fetches and parses the object while honoring ctx. Format is selected
// from the object-key extension and defaults to JSON. Session, transport, and
// read failures wrap [confii.ErrConfigLoad]; parse failures wrap
// [confii.ErrConfigFormat].
func (l *IBMCOSLoader) Load(ctx context.Context) (map[string]any, error) {
	authEndpoint := "https://iam.cloud.ibm.com/identity/token"

	conf := aws.NewConfig().
		WithEndpoint(l.endpointURL).
		WithCredentials(ibmiam.NewStaticCredentials(aws.NewConfig(), authEndpoint, l.apiKey, l.serviceInstanceID)).
		WithS3ForcePathStyle(true)

	sess, err := awssession.NewSession(conf)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	client := ibms3.New(sess)
	output, err := client.GetObjectWithContext(ctx, &ibms3.GetObjectInput{
		Bucket: aws.String(l.bucketName),
		Key:    aws.String(l.objectKey),
	})
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, confii.NewLoadError(l.Source(), err)
	}

	format := loader.FormatFromExtension(l.objectKey)
	if format == loader.FormatUnknown {
		format = loader.FormatJSON
	}

	return loader.ParseContent(data, format, l.Source())
}
