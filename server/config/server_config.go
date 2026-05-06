package config

import (
	"errors"
	"os"
	"path/filepath"

	globallog "github.com/HeaInSeo/go-grpc-kit/log"
	"github.com/spf13/viper"
)

var logger = globallog.Log

const (
	DefaultMaxRequestBytes          = 4 << 20 // 4 MiB
	DefaultGrpcOverheadBytes        = 1 << 20 // 1 MiB
	DefaultMaxSendBytes             = 4 << 20
	DefaultMaxStreams        uint32 = 100
)

// ServerConfig holds gRPC server settings populated from defaults, config file, and environment variables.
type ServerConfig struct {
	MaxRecvMsgSize       int
	MaxSendMsgSize       int
	MaxConcurrentStreams uint32
}

// LoadServerConfig returns a ServerConfig populated from environment variables and an optional config file.
// It uses a local viper instance so it does not affect the caller's global viper state.
// Set GRPC_SERVER_CONFIG_FILE to point to a JSON/YAML config file; otherwise defaults are used.
func LoadServerConfig() *ServerConfig {
	v := viper.New()

	v.SetDefault("max_recv_msg_size", DefaultMaxRequestBytes)
	v.SetDefault("max_send_msg_size", DefaultMaxSendBytes)
	v.SetDefault("max_concurrent_streams", DefaultMaxStreams)

	v.SetEnvPrefix("GRPC")
	v.AutomaticEnv()

	cfgFile := os.Getenv("GRPC_SERVER_CONFIG_FILE")
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if ext := filepath.Ext(cfgFile); ext != "" {
			v.SetConfigType(ext[1:])
		}
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if errors.As(err, &notFound) {
				logger.Warnf("Config file %s not found, using defaults", cfgFile)
			} else {
				logger.Warnf("Error reading config file %s: %v. Using defaults.", cfgFile, err)
			}
		}
	}

	return &ServerConfig{
		MaxRecvMsgSize:       v.GetInt("max_recv_msg_size"),
		MaxSendMsgSize:       v.GetInt("max_send_msg_size"),
		MaxConcurrentStreams: uint32(v.GetUint("max_concurrent_streams")),
	}
}
