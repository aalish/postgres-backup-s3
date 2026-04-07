package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/neelgai/postgres-backup/internal/backup"
	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/retry"
)

type S3Uploader struct {
	bucket      string
	prefix      string
	client      *s3.Client
	uploader    *manager.Uploader
	retryConfig config.RetryConfig
	logger      *slog.Logger
}

type ObjectInfo struct {
	Key          string    `json:"key"`
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	S3URI        string    `json:"s3URI"`
}

func NewS3Uploader(ctx context.Context, cfg config.S3Config, retryConfig config.RetryConfig, logger *slog.Logger) (*S3Uploader, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}

	if cfg.UsesStaticCredentials() {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.UsePathStyle
		if cfg.EndpointURL != "" {
			options.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
	})

	return &S3Uploader{
		bucket:      cfg.Bucket,
		prefix:      cfg.Prefix,
		client:      client,
		uploader:    manager.NewUploader(client),
		retryConfig: retryConfig,
		logger:      logger,
	}, nil
}

func (u *S3Uploader) Upload(ctx context.Context, archive backup.Archive) (string, error) {
	key := BuildObjectKey(u.prefix, archive.Filename)
	u.logger.Info("starting S3 upload", "bucket", u.bucket, "key", key, "size_bytes", archive.Size, "max_attempts", u.retryConfig.MaxAttempts)
	u.logger.Debug("S3 upload details", "bucket", u.bucket, "key", key, "initial_delay", u.retryConfig.InitialDelay.String(), "max_delay", u.retryConfig.MaxDelay.String())

	err := retry.DoWithNotify(ctx, u.retryConfig.MaxAttempts, u.retryConfig.InitialDelay, u.retryConfig.MaxDelay, func(attempt int) error {
		file, err := os.Open(archive.Path)
		if err != nil {
			return fmt.Errorf("open archive for upload: %w", err)
		}
		defer file.Close()

		u.logger.Info("uploading backup to S3", "attempt", attempt, "bucket", u.bucket, "key", key)

		if _, err := u.uploader.Upload(ctx, &s3.PutObjectInput{
			Bucket: aws.String(u.bucket),
			Key:    aws.String(key),
			Body:   file,
		}); err != nil {
			return fmt.Errorf("put object: %w", err)
		}

		if err := u.verifyUpload(ctx, key, archive.Size); err != nil {
			return fmt.Errorf("verify uploaded object: %w", err)
		}

		return nil
	}, func(attempt int, err error, nextDelay time.Duration) {
		u.logger.Warn("S3 upload attempt failed; retry scheduled", "attempt", attempt, "bucket", u.bucket, "key", key, "retry_in", nextDelay.String(), "error", err)
	})
	if err != nil {
		return "", err
	}

	u.logger.Info("S3 upload completed", "bucket", u.bucket, "key", key)
	return fmt.Sprintf("s3://%s/%s", u.bucket, key), nil
}

func (u *S3Uploader) verifyUpload(ctx context.Context, key string, expectedSize int64) error {
	head, err := u.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	remoteSize := aws.ToInt64(head.ContentLength)
	if remoteSize != expectedSize {
		return fmt.Errorf("size mismatch: expected %d bytes, found %d bytes", expectedSize, remoteSize)
	}

	u.logger.Info("S3 upload verified", "bucket", u.bucket, "key", key, "size_bytes", remoteSize)
	return nil
}

func (u *S3Uploader) ListObjects(ctx context.Context) ([]ObjectInfo, error) {
	var objects []ObjectInfo
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(u.bucket),
	}
	if cleanPrefix := strings.Trim(strings.TrimSpace(u.prefix), "/"); cleanPrefix != "" {
		input.Prefix = aws.String(cleanPrefix + "/")
	}

	paginator := s3.NewListObjectsV2Paginator(u.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 objects: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			objects = append(objects, ObjectInfo{
				Key:          key,
				Filename:     path.Base(key),
				Size:         aws.ToInt64(object.Size),
				LastModified: aws.ToTime(object.LastModified),
				S3URI:        fmt.Sprintf("s3://%s/%s", u.bucket, key),
			})
		}
	}

	sort.Slice(objects, func(i, j int) bool {
		return objects[i].LastModified.After(objects[j].LastModified)
	})

	return objects, nil
}

func (u *S3Uploader) DeleteObject(ctx context.Context, key string) error {
	if _, err := u.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete S3 object %s: %w", key, err)
	}

	u.logger.Info("deleted S3 object", "bucket", u.bucket, "key", key)
	return nil
}

func BuildObjectKey(prefix, filename string) string {
	cleanPrefix := strings.Trim(strings.TrimSpace(prefix), "/")
	if cleanPrefix == "" {
		return filename
	}

	return path.Join(cleanPrefix, filename)
}
