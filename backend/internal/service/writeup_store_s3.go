package service

// S3 兼容对象存储后端（MinIO / AWS S3 / 各云厂商 S3 服务）。key = Prefix + rel，
// path-style 恒开（MinIO 与主流私有化部署依赖）。WP zip ≤ MaxWriteupZipBytes，
// 单次 PutObject 即可，无需分片。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3StoreOptions S3 后端连接参数（config.S3Options 的 service 层镜像，
// 避免 service 反向依赖 config 包）。
type S3StoreOptions struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	Prefix    string
}

// errS3ConfigIncomplete s3 后端缺端点/桶（环境变量配齐前拒绝启动）。
var errS3ConfigIncomplete = fmt.Errorf("%w: s3 writeup store requires ACADEMY_WRITEUPS_S3_ENDPOINT and _BUCKET", ErrValidation)

// normalizeS3Endpoint 剥掉用户可能带上的 scheme（minio.New 只要 host[:port]）。
func normalizeS3Endpoint(endpoint string) (string, error) {
	e := strings.TrimSpace(endpoint)
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(e, scheme) {
			e = strings.TrimPrefix(e, scheme)
			break
		}
	}
	e = strings.TrimSuffix(e, "/")
	host := e
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return "", errors.New("s3 endpoint has no host part")
	}
	return e, nil
}

// isHTTPSEndpoint https:// 前缀（或无 scheme 默认）走 TLS，http:// 明文。
func isHTTPSEndpoint(endpoint string) bool {
	return !strings.HasPrefix(strings.TrimSpace(endpoint), "http://")
}

// NewS3WriteupStore 建 S3 后端：缺端点/桶直接报配置错（fail fast，不起服务）。
func NewS3WriteupStore(opt S3StoreOptions) (WriteupStore, error) {
	if opt.Endpoint == "" || opt.Bucket == "" {
		return nil, errS3ConfigIncomplete
	}
	endpoint, err := normalizeS3Endpoint(opt.Endpoint)
	if err != nil {
		return nil, err
	}
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(opt.AccessKey, opt.SecretKey, ""),
		Secure: isHTTPSEndpoint(opt.Endpoint),
		Region: opt.Region,
	})
	if err != nil {
		return nil, err
	}
	return &s3WriteupStore{client: cli, bucket: opt.Bucket, prefix: opt.Prefix}, nil
}

type s3WriteupStore struct {
	client *minio.Client
	bucket string
	prefix string
}

func (s *s3WriteupStore) key(rel string) string {
	if s.prefix == "" {
		return rel
	}
	return s.prefix + "/" + rel
}

func (s *s3WriteupStore) Save(ctx context.Context, rel string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.key(rel), bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/zip"})
	return err
}

func (s *s3WriteupStore) Open(ctx context.Context, rel string) (io.ReadCloser, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.key(rel), minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, ErrNotFound
	}
	// GetObject 是惰性的：Stat 触发首个请求，键不存在在这里暴露。
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, ErrNotFound
	}
	return obj, info.Size, nil
}

func (s *s3WriteupStore) Remove(ctx context.Context, rel string) error {
	// S3 删除不存在的键即成功，天然幂等。
	return s.client.RemoveObject(ctx, s.bucket, s.key(rel), minio.RemoveObjectOptions{})
}

func (s *s3WriteupStore) RemovePrefix(ctx context.Context, prefix string) error {
	target := prefix
	if s.prefix != "" {
		target = s.prefix + "/" + prefix
	}
	objCh := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: target + "/", Recursive: true})
	for obj := range objCh {
		if obj.Err != nil {
			return obj.Err
		}
		if err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}
