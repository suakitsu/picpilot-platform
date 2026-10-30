package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/suakitsu/picpilot-platform/core/mailer"
)

type Edition string

const (
	Platform Edition = "platform"
	Personal Edition = "personal"
)

type Config struct {
	Edition                           Edition
	Address                           string
	DatabaseURL                       string
	PublicBaseURL                     string
	DataDir                           string
	WebDir                            string
	AssetStorageBackend               string
	ObjectStorageEndpoint             string
	ObjectStorageBucket               string
	ObjectStorageRegion               string
	ObjectStoragePrefix               string
	ObjectStorageAccess               string
	ObjectStorageSecret               string
	ObjectStorageSession              string
	ObjectStorageInsecure             bool
	ObjectStorageVirtual              bool
	ObjectStorageOpTimeoutSeconds     int
	ObjectStorageUploadTimeoutSeconds int
	ObjectStorageReadTimeoutSeconds   int
	ObjectStorageMaxUploads           int
	HEIFEnabled                       bool
	HEIFVIPSPath                      string
	HEIFVIPSHeaderPath                string
	HEIFTempDir                       string
	HEIFTimeoutSeconds                int
	HEIFMaxConcurrent                 int
	AllowedOrigins                    []string
	TrustedProxyCIDRs                 []netip.Prefix
	AuthKey                           []byte
	EncryptionKey                     []byte
	WorkerPepper                      []byte
	WorkerSigningKey                  []byte
	WorkerGraceSeconds                int
	WorkerInputTTLSeconds             int
	SetupToken                        string
	DemoMode                          bool
	// AllowPrivateProviders is an internal trust bit for explicitly configured
	// Provider endpoints. It is not a user-facing environment setting.
	AllowPrivateProviders bool
	RegistrationEnabled   bool
	SessionTTL            time.Duration
	ShutdownTimeout       time.Duration
	SMTP                  mailer.Options
	GitHubClientID        string
	GitHubClientSecret    string
	TurnstileSiteKey      string
	TurnstileSecret       string
}

type secrets struct {
	Auth          string `json:"auth_key"`
	Encryption    string `json:"encryption_key"`
	WorkerPepper  string `json:"worker_pepper"`
	WorkerSigning string `json:"worker_signing_key"`
	Setup         string `json:"setup_token"`
}

// Load reads configuration for a build-selected edition; an HTTP request can never change it.
func Load(edition Edition) (Config, error) {
	if edition != Platform && edition != Personal {
		return Config{}, errors.New("invalid application edition")
	}
	dir, err := filepath.Abs(value("PICPILOT_DATA_DIR", "./data"))
	if err != nil {
		return Config{}, errors.New("invalid data directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Config{}, errors.New("cannot initialize data directory")
	}
	stored, err := loadSecrets(dir)
	if err != nil {
		return Config{}, err
	}
	publicURL := strings.TrimRight(value("PICPILOT_PUBLIC_URL", "http://localhost:8080"), "/")
	c := Config{
		Edition: edition, Address: value("PICPILOT_LISTEN", "127.0.0.1:8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"), PublicBaseURL: publicURL,
		DataDir: dir, WebDir: os.Getenv("PICPILOT_WEB_DIR"),
		AssetStorageBackend:   value("PICPILOT_ASSET_STORAGE_BACKEND", "local"),
		ObjectStorageEndpoint: os.Getenv("PICPILOT_OBJECT_STORAGE_ENDPOINT"),
		ObjectStorageBucket:   os.Getenv("PICPILOT_OBJECT_STORAGE_BUCKET"),
		ObjectStorageRegion:   value("PICPILOT_OBJECT_STORAGE_REGION", "us-east-1"),
		ObjectStoragePrefix:   value("PICPILOT_OBJECT_STORAGE_PREFIX", "picpilot"),
		ObjectStorageAccess:   os.Getenv("PICPILOT_OBJECT_STORAGE_ACCESS_KEY"),
		ObjectStorageSecret:   os.Getenv("PICPILOT_OBJECT_STORAGE_SECRET_KEY"),
		ObjectStorageSession:  os.Getenv("PICPILOT_OBJECT_STORAGE_SESSION_TOKEN"),
		ObjectStorageInsecure: boolean("PICPILOT_OBJECT_STORAGE_ALLOW_INSECURE_HTTP", false),
		ObjectStorageVirtual:  boolean("PICPILOT_OBJECT_STORAGE_VIRTUAL_HOST_STYLE", false),
		HEIFEnabled:           boolean("PICPILOT_HEIF_ENABLED", false),
		HEIFVIPSPath:          os.Getenv("PICPILOT_HEIF_VIPS_PATH"),
		HEIFVIPSHeaderPath:    os.Getenv("PICPILOT_HEIF_VIPSHEADER_PATH"),
		HEIFTempDir:           value("PICPILOT_HEIF_TEMP_DIR", filepath.Join(dir, "heif-tmp")),
		SetupToken:            value("PICPILOT_SETUP_TOKEN", stored.Setup),
		DemoMode:              boolean("PICPILOT_DEMO_MODE", false),
		// Configured Provider endpoints are explicit operator/user choices. Their
		// HTTP(S) connectivity is checked by the provider probe and request path;
		// arbitrary user-supplied reference URLs keep the strict gateway below.
		AllowPrivateProviders: true,
		RegistrationEnabled:   edition == Platform && boolean("PICPILOT_REGISTRATION_ENABLED", false),
		SessionTTL:            2 * time.Hour, ShutdownTimeout: 20 * time.Second,
		GitHubClientID: os.Getenv("PICPILOT_GITHUB_CLIENT_ID"), GitHubClientSecret: os.Getenv("PICPILOT_GITHUB_CLIENT_SECRET"),
		TurnstileSiteKey: os.Getenv("PICPILOT_TURNSTILE_SITE_KEY"), TurnstileSecret: os.Getenv("PICPILOT_TURNSTILE_SECRET"),
	}
	if c.WorkerGraceSeconds, err = boundedInt("PICPILOT_WORKER_LEASE_GRACE_SECONDS", 15, 5, 120); err != nil {
		return Config{}, err
	}
	if c.WorkerInputTTLSeconds, err = boundedInt("PICPILOT_WORKER_INPUT_TTL_SECONDS", 300, 30, 600); err != nil {
		return Config{}, err
	}
	if c.AssetStorageBackend != "local" && c.AssetStorageBackend != "object" {
		return Config{}, errors.New("PICPILOT_ASSET_STORAGE_BACKEND must be local or object")
	}
	if c.ObjectStorageOpTimeoutSeconds, err = boundedInt("PICPILOT_OBJECT_STORAGE_OPERATION_TIMEOUT_SECONDS", 30, 1, 300); err != nil {
		return Config{}, err
	}
	if c.ObjectStorageUploadTimeoutSeconds, err = boundedInt("PICPILOT_OBJECT_STORAGE_UPLOAD_TIMEOUT_SECONDS", 900, 1, 3600); err != nil {
		return Config{}, err
	}
	if c.ObjectStorageReadTimeoutSeconds, err = boundedInt("PICPILOT_OBJECT_STORAGE_READ_TIMEOUT_SECONDS", 600, 1, 3600); err != nil {
		return Config{}, err
	}
	if c.ObjectStorageMaxUploads, err = boundedInt("PICPILOT_OBJECT_STORAGE_MAX_CONCURRENT_UPLOADS", 2, 1, 8); err != nil {
		return Config{}, err
	}
	if c.HEIFTimeoutSeconds, err = boundedInt("PICPILOT_HEIF_TIMEOUT_SECONDS", 30, 1, 30); err != nil {
		return Config{}, err
	}
	if c.HEIFMaxConcurrent, err = boundedInt("PICPILOT_HEIF_MAX_CONCURRENT", 2, 1, 4); err != nil {
		return Config{}, err
	}
	if c.HEIFEnabled {
		if !filepath.IsAbs(c.HEIFTempDir) {
			return Config{}, errors.New("PICPILOT_HEIF_TEMP_DIR must be absolute")
		}
		if err := os.MkdirAll(c.HEIFTempDir, 0700); err != nil {
			return Config{}, errors.New("cannot initialize HEIF temporary directory")
		}
	}
	port, err := strconv.Atoi(value("PICPILOT_SMTP_PORT", "587"))
	if err != nil {
		return Config{}, errors.New("PICPILOT_SMTP_PORT must be an integer")
	}
	c.SMTP = mailer.Options{Host: os.Getenv("PICPILOT_SMTP_HOST"), Port: port, Username: os.Getenv("PICPILOT_SMTP_USERNAME"), Password: os.Getenv("PICPILOT_SMTP_PASSWORD"), From: os.Getenv("PICPILOT_SMTP_FROM"), TLSMode: value("PICPILOT_SMTP_TLS_MODE", "starttls")}
	if _, err = mailer.New(c.SMTP); err != nil {
		return Config{}, err
	}
	if (c.TurnstileSiteKey == "") != (c.TurnstileSecret == "") {
		return Config{}, errors.New("both Turnstile site and secret keys are required")
	}
	if (c.GitHubClientID == "") != (c.GitHubClientSecret == "") {
		return Config{}, errors.New("both GitHub OAuth client ID and secret are required")
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required; run the quick-start initializer first")
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Config{}, errors.New("PICPILOT_PUBLIC_URL must be an HTTP(S) origin")
	}
	for _, item := range strings.Split(os.Getenv("PICPILOT_ALLOWED_ORIGINS"), ",") {
		if origin := strings.TrimSpace(item); origin != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	c.AllowedOrigins = append(c.AllowedOrigins, c.PublicBaseURL)
	for _, raw := range strings.Split(os.Getenv("PICPILOT_TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || prefix.Bits() == 0 {
			return Config{}, errors.New("PICPILOT_TRUSTED_PROXY_CIDRS requires explicit non-universal CIDRs")
		}
		c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, prefix.Masked())
	}
	for _, entry := range []struct {
		name, fallback string
		target         *[]byte
	}{
		{"PICPILOT_AUTH_KEY", stored.Auth, &c.AuthKey},
		{"PICPILOT_ENCRYPTION_KEY", stored.Encryption, &c.EncryptionKey},
		{"PICPILOT_WORKER_TOKEN_PEPPER", stored.WorkerPepper, &c.WorkerPepper},
		{"PICPILOT_WORKER_SIGNING_SECRET", stored.WorkerSigning, &c.WorkerSigningKey},
	} {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(value(entry.name, entry.fallback))
		if decodeErr != nil || len(decoded) != 32 {
			return Config{}, fmt.Errorf("%s must be an unpadded URL-safe base64 32-byte secret", entry.name)
		}
		*entry.target = decoded
	}
	if len(c.SetupToken) < 32 {
		return Config{}, errors.New("setup token must contain at least 32 characters")
	}
	return c, nil
}

func value(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func boolean(name string, fallback bool) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return v
}

func boundedInt(name string, fallback, minimum, maximum int) (int, error) {
	n, err := strconv.Atoi(value(name, strconv.Itoa(fallback)))
	if err != nil || n < minimum || n > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return n, nil
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func loadSecrets(dir string) (secrets, error) {
	path := filepath.Join(dir, "instance-secrets.json")
	if data, err := os.ReadFile(path); err == nil {
		if err := protectPrivateFile(path); err != nil {
			return secrets{}, errors.New("cannot protect instance secrets")
		}
		var s secrets
		if json.Unmarshal(data, &s) != nil {
			return secrets{}, errors.New("instance secrets are invalid; restore them from your backup")
		}
		return s, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return secrets{}, errors.New("cannot read instance secrets")
	}
	var s secrets
	for _, dst := range []*string{&s.Auth, &s.Encryption, &s.WorkerPepper, &s.WorkerSigning, &s.Setup} {
		v, err := randomSecret()
		if err != nil {
			return secrets{}, errors.New("secure random generation failed")
		}
		*dst = v
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	tmp, err := os.CreateTemp(dir, ".secrets-*")
	if err != nil {
		return secrets{}, errors.New("cannot persist instance secrets")
	}
	defer os.Remove(tmp.Name())
	if err := protectPrivateFile(tmp.Name()); err != nil {
		tmp.Close()
		return secrets{}, errors.New("cannot protect instance secrets")
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return secrets{}, errors.New("cannot persist instance secrets")
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return secrets{}, errors.New("cannot persist instance secrets")
	}
	if err = tmp.Close(); err != nil {
		return secrets{}, errors.New("cannot persist instance secrets")
	}
	// Link is atomic and refuses to overwrite a competing instance's keys.
	if err = os.Link(tmp.Name(), path); errors.Is(err, os.ErrExist) {
		return loadSecrets(dir)
	}
	if err != nil {
		return secrets{}, errors.New("cannot atomically persist instance secrets")
	}
	return s, nil
}
