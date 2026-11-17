package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/line-age/georef/internal/storage"
)

const (
	minioRootUser     = "testkey"
	minioRootPassword = "testsecret"
	testBucket        = "georef-test"
	testSheetID       = "01JEMKR9VXYZ"
)

func TestS3Store(t *testing.T) {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "cgr.dev/chainguard/minio:latest",
			Env: map[string]string{
				"MINIO_ROOT_USER":     minioRootUser,
				"MINIO_ROOT_PASSWORD": minioRootPassword,
			},
			Cmd:          []string{"server", "/data"},
			ExposedPorts: []string{"9000/tcp"},
			WaitingFor: wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start minio container: %v", err)
	}
	t.Cleanup(func() { ctr.Terminate(ctx) }) //nolint:errcheck

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("get container host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("get container port: %v", err)
	}
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	// Create the bucket before running any tests.
	if err := createBucket(ctx, endpoint, testBucket); err != nil {
		t.Fatalf("create test bucket: %v", err)
	}

	// Set AWS credential env vars so NewS3Store picks them up via the default chain.
	t.Setenv("AWS_ACCESS_KEY_ID", minioRootUser)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioRootPassword)

	store, err := storage.NewS3Store(ctx, storage.S3Config{
		Bucket:   testBucket,
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}

	t.Run("save_and_open_original_scan", func(t *testing.T) {
		content := "original scan bytes"

		if _, err := store.SaveOriginalScan(ctx, testSheetID, strings.NewReader(content)); err != nil {
			t.Fatalf("SaveOriginalScan: %v", err)
		}

		rc, err := store.OpenOriginalScan(ctx, testSheetID)
		if err != nil {
			t.Fatalf("OpenOriginalScan: %v", err)
		}
		defer rc.Close()

		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(got) != content {
			t.Fatalf("content mismatch: got %q, want %q", got, content)
		}
	})

	t.Run("original_scan_path_returns_s3_uri", func(t *testing.T) {
		path, err := store.OriginalScanPath(ctx, testSheetID)
		if err != nil {
			t.Fatalf("OriginalScanPath: %v", err)
		}
		want := "s3://" + testBucket + "/sheets/" + testSheetID + "/original"
		if path != want {
			t.Fatalf("path mismatch: got %q, want %q", path, want)
		}
	})

	t.Run("save_pyramidal_tiff_and_get_path", func(t *testing.T) {
		content := []byte{0x49, 0x49, 0x2A, 0x00} // TIFF little-endian magic bytes
		tiffID := "01JEMKR9VTIF"

		if _, err := store.SavePyramidalTIFF(ctx, tiffID, bytes.NewReader(content)); err != nil {
			t.Fatalf("SavePyramidalTIFF: %v", err)
		}

		path, err := store.PyramidalTIFFPath(ctx, tiffID)
		if err != nil {
			t.Fatalf("PyramidalTIFFPath: %v", err)
		}
		want := "s3://" + testBucket + "/sheets/" + tiffID + "/pyramid.tif"
		if path != want {
			t.Fatalf("path mismatch: got %q, want %q", path, want)
		}
	})

	t.Run("save_returns_key", func(t *testing.T) {
		scanID := "01JEMKR9VKEY"

		key, err := store.SaveOriginalScan(ctx, scanID, strings.NewReader("x"))
		if err != nil {
			t.Fatalf("SaveOriginalScan: %v", err)
		}
		want := "sheets/" + scanID + "/original"
		if key != want {
			t.Fatalf("key mismatch: got %q, want %q", key, want)
		}
	})

	t.Run("delete_original_scan", func(t *testing.T) {
		delID := "01JEMKR9VDEL"
		if _, err := store.SaveOriginalScan(ctx, delID, strings.NewReader("to delete")); err != nil {
			t.Fatalf("SaveOriginalScan: %v", err)
		}
		if err := store.DeleteOriginalScan(ctx, delID); err != nil {
			t.Fatalf("DeleteOriginalScan: %v", err)
		}

		// After deletion, OpenOriginalScan must fail.
		_, err := store.OpenOriginalScan(ctx, delID)
		if err == nil {
			t.Fatal("expected error opening deleted scan, got nil")
		}
	})

	t.Run("open_missing_scan_returns_error", func(t *testing.T) {
		_, err := store.OpenOriginalScan(ctx, "does-not-exist")
		if err == nil {
			t.Fatal("expected error opening missing scan, got nil")
		}
	})
}

// createBucket creates the named bucket in a MinIO instance at the given endpoint.
// MinIO starts empty, so this must be called before any object operations.
func createBucket(ctx context.Context, endpoint, bucket string) error {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			minioRootUser, minioRootPassword, "",
		)),
	)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}
