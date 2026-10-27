package storage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

var (
	ErrObjectConfiguration = errors.New("invalid object storage configuration")
	ErrObjectUnavailable   = errors.New("object storage operation failed")
	ErrObjectChanged       = errors.New("object changed; operation refused")
	ErrObjectRedirect      = errors.New("object storage redirect refused")
	ErrObjectReadLimit     = errors.New("invalid object read limit")
)

// ObjectOptions configures an optional, private S3-compatible backend. The
// bucket must already exist. No bucket, ACL, policy or lifecycle is modified.
// Prefix must be dedicated to this application's asset namespace. StagingDir
// must be an absolute, private directory outside the source checkout.
//
// Credentials are explicit: environment, instance-role and metadata credential
// discovery are deliberately not used. Endpoint has the form https://host:port.
// HTTP requires explicit opt-in and is only appropriate on a trusted network.
type ObjectOptions struct {
	Endpoint             string
	Bucket               string
	Region               string
	Prefix               string
	AccessKey            string `json:"-"`
	SecretKey            string `json:"-"`
	SessionToken         string `json:"-"`
	StagingDir           string
	AllowInsecureHTTP    bool
	VirtualHostStyle     bool
	TLSRootCAs           *x509.CertPool `json:"-"`
	OperationTimeout     time.Duration
	UploadTimeout        time.Duration
	ReadTimeout          time.Duration
	MaxConcurrentUploads int
}

// Formatting must not expose even malformed endpoint userinfo or credentials.
func (ObjectOptions) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "storage.ObjectOptions{redacted}")
}
func (ObjectOptions) MarshalJSON() ([]byte, error) {
	return []byte(`{"backend":"s3","redacted":true}`), nil
}

// Object never returns remote URLs, SDK errors or credential-bearing requests.
// All reads remain behind the application's authorization/signed-read handlers.
type Object struct {
	client           *minio.Client
	http             *http.Client
	transport        *http.Transport
	bucket           string
	prefix           string
	staging          *Local
	ctx              context.Context
	cancel           context.CancelFunc
	closeOnce        sync.Once
	closedErr        error
	slots            chan struct{}
	operationTimeout time.Duration
	uploadTimeout    time.Duration
	readTimeout      time.Duration
}

func (*Object) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "storage.Object{redacted}") }

var objectPrefixPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)*$`)
var objectRegionPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// NewObject validates configuration and opens private staging storage, but does
// not contact the endpoint. Check can be used for an explicit startup probe.
func NewObject(options ObjectOptions) (*Object, error) {
	o, endpoint, err := validateObjectOptions(options)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.TLSRootCAs},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.operationTimeout(),
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          16, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 32,
		MaxResponseHeaderBytes: 64 << 10,
		DisableCompression:     true,
	}
	guard := &objectTransport{base: transport, scheme: endpoint.Scheme, bucket: o.Bucket, prefix: o.Prefix + "/", virtual: o.VirtualHostStyle}
	lookup := minio.BucketLookupPath
	if o.VirtualHostStyle {
		lookup = minio.BucketLookupDNS
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(o.AccessKey, o.SecretKey, o.SessionToken),
		Secure: endpoint.Scheme == "https", Region: o.Region,
		Transport: guard, BucketLookup: lookup, MaxRetries: 1,
	})
	if err != nil {
		return nil, ErrObjectConfiguration
	}
	// Resolve SDK regional/virtual-host addressing without making a request. Only
	// this exact origin is permitted; neither redirects nor ambient proxies apply.
	probe, err := client.PresignedHeadObject(context.Background(), o.Bucket, o.Prefix+"/00/asset_00000000000000000000000000000000.png", time.Minute, nil)
	if err != nil {
		return nil, ErrObjectConfiguration
	}
	guard.host = probe.Host
	namespace := sha256.Sum256([]byte(endpoint.String() + "\x00" + o.Bucket + "\x00" + o.Prefix))
	staging, err := NewLocal(filepath.Join(o.StagingDir, "picpilot-object-"+hex.EncodeToString(namespace[:12])))
	if err != nil {
		return nil, ErrObjectUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Object{
		client: client, http: &http.Client{Transport: guard, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		transport: transport, bucket: o.Bucket, prefix: o.Prefix + "/", staging: staging,
		ctx: ctx, cancel: cancel, slots: make(chan struct{}, o.MaxConcurrentUploads),
		operationTimeout: o.OperationTimeout, uploadTimeout: o.UploadTimeout, readTimeout: o.ReadTimeout,
	}, nil
}

func (o ObjectOptions) operationTimeout() time.Duration { return o.OperationTimeout }

func validateObjectOptions(o ObjectOptions) (ObjectOptions, *url.URL, error) {
	bad := func() (ObjectOptions, *url.URL, error) { return ObjectOptions{}, nil, ErrObjectConfiguration }
	u, err := url.Parse(o.Endpoint)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return bad()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && o.AllowInsecureHTTP) {
		return bad()
	}
	if s3utils.CheckValidBucketNameStrict(o.Bucket) != nil || strings.HasSuffix(o.Bucket, "--x-s3") {
		return bad()
	}
	if o.VirtualHostStyle && (net.ParseIP(u.Hostname()) != nil || strings.Contains(o.Bucket, ".")) {
		return bad()
	}
	if len(o.Prefix) > 200 || !objectPrefixPattern.MatchString(o.Prefix) || !filepath.IsAbs(o.StagingDir) {
		return bad()
	}
	for _, value := range []string{o.AccessKey, o.SecretKey, o.SessionToken} {
		if len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
			return bad()
		}
	}
	if strings.TrimSpace(o.AccessKey) == "" || strings.TrimSpace(o.SecretKey) == "" {
		return bad()
	}
	if o.Region == "" {
		o.Region = "us-east-1"
	}
	if !objectRegionPattern.MatchString(o.Region) || o.Region == "snowball" {
		return bad()
	}
	if o.OperationTimeout == 0 {
		o.OperationTimeout = 30 * time.Second
	}
	if o.UploadTimeout == 0 {
		o.UploadTimeout = 15 * time.Minute
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = 10 * time.Minute
	}
	if o.OperationTimeout < time.Millisecond || o.OperationTimeout > 5*time.Minute || o.UploadTimeout < time.Millisecond || o.UploadTimeout > time.Hour || o.ReadTimeout < time.Millisecond || o.ReadTimeout > time.Hour {
		return bad()
	}
	if o.MaxConcurrentUploads == 0 {
		o.MaxConcurrentUploads = 2
	}
	if o.MaxConcurrentUploads < 1 || o.MaxConcurrentUploads > 8 {
		return bad()
	}
	u.Path = ""
	return o, u, nil
}

func (s *Object) operation(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	if s.ctx.Err() != nil {
		return nil, nil, fs.ErrClosed
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }, nil
}

// Check verifies that the already-provisioned bucket is accessible. It never
// creates it or changes its policy. Bucket privacy remains an operator invariant.
func (s *Object) Check(parent context.Context) error {
	ctx, cancel, err := s.operation(parent, s.operationTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return objectError(err)
	}
	if !exists {
		return fs.ErrNotExist
	}
	return nil
}

func (s *Object) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.transport.CloseIdleConnections()
		s.closedErr = objectError(s.staging.Close())
	})
	return s.closedErr
}

func (s *Object) acquire(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func objectError(err error) error {
	if err == nil {
		return nil
	}
	for _, safe := range []error{fs.ErrClosed, fs.ErrNotExist, fs.ErrExist, fs.ErrPermission, fs.ErrInvalid, context.Canceled, context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF, ErrObjectRedirect, ErrObjectChanged, ErrObjectUnavailable, ErrInvalidImage, ErrImageTooLarge, ErrInvalidMedia, ErrMediaTooLarge} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return context.DeadlineExceeded
	}
	remote := minio.ToErrorResponse(err)
	switch remote.Code {
	case "NoSuchKey", "NoSuchObject", "NoSuchBucket", "NotFound":
		return fs.ErrNotExist
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken":
		return fs.ErrPermission
	case "PreconditionFailed", "ConditionalRequestConflict":
		return ErrObjectChanged
	case "InvalidRange":
		return fs.ErrInvalid
	}
	return ErrObjectUnavailable
}

var _ Files = (*Object)(nil)
