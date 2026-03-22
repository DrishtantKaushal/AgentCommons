package config

import "time"

// Config holds daemon configuration.
type Config struct {
	Port              int           `json:"port"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout"`
	ReaperInterval    time.Duration `json:"reaper_interval"`
	GracePeriod       time.Duration `json:"grace_period"`
	WALCheckpoint     time.Duration `json:"wal_checkpoint_interval"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Port:              7390,
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
		ReaperInterval:    10 * time.Second,
		GracePeriod:       60 * time.Second,
		WALCheckpoint:     5 * time.Minute,
	}
}
