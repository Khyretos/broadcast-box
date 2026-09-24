package clips

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var ErrNotFound = errors.New("clip not found")

// Storage stores clip files under keys of the form "<streamKey>/<file>"
type Storage interface {
	Save(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Read(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
	// Serve responds with the file, as a download when downloadName is set
	Serve(w http.ResponseWriter, r *http.Request, key string, downloadName string)
	Name() string
}

// Local directory storage

type localStorage struct {
	root string
}

func NewLocalStorage(root string) (Storage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("creating clip directory: %w", err)
	}
	return &localStorage{root: root}, nil
}

func (s *localStorage) Name() string { return "local:" + s.root }

func (s *localStorage) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}

func (s *localStorage) Save(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	target := s.path(key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	// Write to a temporary file first so a crash never leaves a partial clip
	file, err := os.CreateTemp(filepath.Dir(target), ".partial-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()

	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(file.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(file.Name(), target)
}

func (s *localStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *localStorage) List(_ context.Context, prefix string) ([]string, error) {
	entries, err := os.ReadDir(s.path(prefix))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			keys = append(keys, path.Join(prefix, entry.Name()))
		}
	}
	return keys, nil
}

func (s *localStorage) Delete(_ context.Context, key string) error {
	err := os.Remove(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (s *localStorage) Serve(w http.ResponseWriter, r *http.Request, key string, downloadName string) {
	file, err := os.Open(s.path(key))
	if err != nil {
		http.Error(w, ErrNotFound.Error(), http.StatusNotFound)
		return
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setContentHeaders(w, key, downloadName)
	http.ServeContent(w, r, "", stat.ModTime(), file)
}

func setContentHeaders(w http.ResponseWriter, key string, downloadName string) {
	w.Header().Set("Content-Type", contentTypeFor(key))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if downloadName != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": downloadName}))
	}
}

func contentTypeFor(key string) string {
	switch path.Ext(key) {
	case ".mkv":
		return "video/x-matroska"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

// S3 compatible storage

type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	Prefix    string
	UseSSL    bool
}

type s3Storage struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewS3Storage(config S3Config) (Storage, error) {
	endpoint := config.Endpoint
	useSSL := config.UseSSL
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		endpoint = parsed.Host
		useSSL = parsed.Scheme == "https"
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: useSSL,
		Region: config.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}

	return &s3Storage{client: client, bucket: config.Bucket, prefix: strings.Trim(config.Prefix, "/")}, nil
}

func (s *s3Storage) Name() string { return "s3:" + s.bucket + "/" + s.prefix }

func (s *s3Storage) object(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *s3Storage) Save(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.object(key), reader, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *s3Storage) Read(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, s.object(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = object.Close() }()

	data, err := io.ReadAll(object)
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *s3Storage) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.object(prefix) + "/"}) {
		if object.Err != nil {
			return nil, object.Err
		}
		keys = append(keys, strings.TrimPrefix(strings.TrimPrefix(object.Key, s.prefix), "/"))
	}
	return keys, nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.object(key), minio.RemoveObjectOptions{})
}

// Redirects to a short lived presigned URL, so video is served by S3 directly
func (s *s3Storage) Serve(w http.ResponseWriter, r *http.Request, key string, downloadName string) {
	params := url.Values{}
	params.Set("response-content-type", contentTypeFor(key))
	if downloadName != "" {
		params.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": downloadName}))
	}

	presigned, err := s.client.PresignedGetObject(r.Context(), s.bucket, s.object(key), time.Hour, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, presigned.String(), http.StatusFound)
}
