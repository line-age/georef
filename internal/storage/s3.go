package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	keyOriginalScan  = "sheets/%s/original"
	keyPyramidalTIFF = "sheets/%s/pyramid.tif"
	s3URITemplate    = "s3://%s/%s"
)

// S3Config holds the parameters for the S3 storage backend.
type S3Config struct {
	Bucket   string
	Region   string // defaults to "us-east-1" when empty
	Endpoint string // optional; set for S3-compatible services such as MinIO
}

type s3Store struct {
	s3Client *s3.Client
	uploader *manager.Uploader
	bucket   string
}

// NewS3Store creates an S3-backed Store.
// Credentials are resolved via the standard AWS credential chain:
// environment variables (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY),
// shared credentials file, or instance metadata.
func NewS3Store(ctx context.Context, cfg S3Config) (Store, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.UsePathStyle = true // required for MinIO and other path-style S3 services
		}
	})

	return &s3Store{
		s3Client: s3Client,
		uploader: manager.NewUploader(s3Client),
		bucket:   cfg.Bucket,
	}, nil
}

func (store *s3Store) SaveOriginalScan(ctx context.Context, id string, reader io.Reader) (string, error) {
	key := fmt.Sprintf(keyOriginalScan, id)
	if _, err := store.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
		Body:   reader,
	}); err != nil {
		return "", fmt.Errorf("upload original scan %s: %w", id, err)
	}
	return key, nil
}

func (store *s3Store) DeleteOriginalScan(ctx context.Context, id string) error {
	key := fmt.Sprintf(keyOriginalScan, id)
	if _, err := store.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete original scan %s: %w", id, err)
	}
	return nil
}

func (store *s3Store) OpenOriginalScan(ctx context.Context, id string) (io.ReadCloser, error) {
	key := fmt.Sprintf(keyOriginalScan, id)
	output, err := store.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get original scan %s: %w", id, err)
	}
	return output.Body, nil
}

func (store *s3Store) OriginalScanPath(_ context.Context, id string) (string, error) {
	return fmt.Sprintf(s3URITemplate, store.bucket, fmt.Sprintf(keyOriginalScan, id)), nil
}

func (store *s3Store) SavePyramidalTIFF(ctx context.Context, id string, reader io.Reader) (string, error) {
	key := fmt.Sprintf(keyPyramidalTIFF, id)
	if _, err := store.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
		Body:   reader,
	}); err != nil {
		return "", fmt.Errorf("upload pyramidal tiff %s: %w", id, err)
	}
	return key, nil
}

func (store *s3Store) PyramidalTIFFPath(_ context.Context, id string) (string, error) {
	return fmt.Sprintf(s3URITemplate, store.bucket, fmt.Sprintf(keyPyramidalTIFF, id)), nil
}
