package consistency

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	envDefaultMode      = "PLASMOD_CONSISTENCY_DEFAULT_MODE"
	envBoundedMaxLag    = "PLASMOD_CONSISTENCY_BOUNDED_MAX_LAG"
	envQueueSize        = "PLASMOD_CONSISTENCY_QUEUE_SIZE"
	envWorkers          = "PLASMOD_CONSISTENCY_WORKERS"
	envMaxRetries       = "PLASMOD_CONSISTENCY_MAX_RETRIES"
	envRetryBase        = "PLASMOD_CONSISTENCY_RETRY_BASE"
	envRetryMax         = "PLASMOD_CONSISTENCY_RETRY_MAX"
	envQueryTimeout     = "PLASMOD_CONSISTENCY_QUERY_TIMEOUT"
	envShutdownTimeout  = "PLASMOD_CONSISTENCY_SHUTDOWN_TIMEOUT"
	envCheckpointPath   = "PLASMOD_CONSISTENCY_CHECKPOINT_PATH"
	checkpointFileName  = "consistency_checkpoint.json"
	defaultQueueSize    = 4096
	defaultWorkers      = 4
	defaultMaxRetries   = 8
	defaultBoundedLag   = time.Second
	defaultRetryBase    = 25 * time.Millisecond
	defaultRetryMax     = 2 * time.Second
	defaultQueryTimeout = 30 * time.Second
	defaultShutdown     = 30 * time.Second
)

// Config controls consistency admission, projection, retries, and lifecycle.
type Config struct {
	DefaultMode                 Mode
	BoundedMaxLag               time.Duration
	QueueSize                   int
	Workers                     int
	MaxRetries                  int
	RetryBaseDelay              time.Duration
	RetryMaxDelay               time.Duration
	QueryWaitTimeout            time.Duration
	ShutdownTimeout             time.Duration
	CheckpointPath              string
	BootstrapCheckpointAtLatest bool
}

// DefaultConfig returns safe compatibility defaults for an in-memory runtime.
func DefaultConfig() Config {
	return Config{
		DefaultMode:      StrictVisible,
		BoundedMaxLag:    defaultBoundedLag,
		QueueSize:        defaultQueueSize,
		Workers:          defaultWorkers,
		MaxRetries:       defaultMaxRetries,
		RetryBaseDelay:   defaultRetryBase,
		RetryMaxDelay:    defaultRetryMax,
		QueryWaitTimeout: defaultQueryTimeout,
		ShutdownTimeout:  defaultShutdown,
	}
}

// ConfigFromEnv overlays supported environment variables on safe defaults.
func ConfigFromEnv(dataDir string, persistent bool) Config {
	cfg := DefaultConfig()
	if raw := strings.TrimSpace(os.Getenv(envDefaultMode)); raw != "" {
		if mode, err := ParseMode(raw); err == nil {
			cfg.DefaultMode = mode
		} else {
			log.Printf("[consistency] invalid %s=%q; using %s", envDefaultMode, raw, cfg.DefaultMode)
		}
	}
	cfg.BoundedMaxLag = durationFromEnv(envBoundedMaxLag, cfg.BoundedMaxLag)
	cfg.QueueSize = positiveIntFromEnv(envQueueSize, cfg.QueueSize)
	cfg.Workers = positiveIntFromEnv(envWorkers, cfg.Workers)
	cfg.MaxRetries = positiveIntFromEnv(envMaxRetries, cfg.MaxRetries)
	cfg.RetryBaseDelay = durationFromEnv(envRetryBase, cfg.RetryBaseDelay)
	cfg.RetryMaxDelay = durationFromEnv(envRetryMax, cfg.RetryMaxDelay)
	if cfg.RetryMaxDelay < cfg.RetryBaseDelay {
		cfg.RetryMaxDelay = cfg.RetryBaseDelay
	}
	cfg.QueryWaitTimeout = durationFromEnv(envQueryTimeout, cfg.QueryWaitTimeout)
	cfg.ShutdownTimeout = durationFromEnv(envShutdownTimeout, cfg.ShutdownTimeout)

	dataDir = strings.TrimSpace(dataDir)
	if persistent && dataDir != "" && dataDir != ":memory:" {
		cfg.CheckpointPath = strings.TrimSpace(os.Getenv(envCheckpointPath))
		if cfg.CheckpointPath == "" {
			cfg.CheckpointPath = filepath.Join(dataDir, checkpointFileName)
		}
		cfg.BootstrapCheckpointAtLatest = true
	}
	return cfg
}

func positiveIntFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		log.Printf("[consistency] invalid %s=%q; using %d", key, raw, fallback)
		return fallback
	}
	return value
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		log.Printf("[consistency] invalid %s=%q; using %s", key, raw, fallback)
		return fallback
	}
	return value
}
