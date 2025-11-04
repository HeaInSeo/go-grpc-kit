package config

import (
	"errors"
	"os"
	"path/filepath"

	globallog "github.com/seoyhaein/go-grpc-kit/log"
	"github.com/spf13/viper"
)

var logger = globallog.Log

const (
	DefaultMaxRequestBytes          = 4 << 20 // 예: 4MiB
	DefaultGrpcOverheadBytes        = 1 << 20 // 예: 1MiB
	DefaultMaxSendBytes             = 4 << 20
	DefaultMaxStreams        uint32 = 100
)

// ServerConfig holds the final gRPC server settings
// populated from defaults, config file, and environment variables.
type ServerConfig struct {
	MaxRecvMsgSize       int
	MaxSendMsgSize       int
	MaxConcurrentStreams uint32
	// TLS, interceptor 등 추가 필드 시 주석으로 설명 또는 별도 설정이 필요합니다.
}

func init() {
	// 1) 기본값(Default)
	viper.SetDefault("max_recv_msg_size", DefaultMaxRequestBytes)
	viper.SetDefault("max_send_msg_size", DefaultMaxSendBytes)
	viper.SetDefault("max_concurrent_streams", DefaultMaxStreams)

	// 2) 환경 변수 바인딩
	viper.SetEnvPrefix("GRPC") // GRPC_MAX_RECV_MSG_SIZE 등
	viper.AutomaticEnv()       // 자동으로 ENV → key 매핑

	// 3) 설정 파일 처리
	cfgFile := os.Getenv("GRPC_SERVER_CONFIG_FILE") // 파일 설정파일 위치
	if cfgFile != "" {
		// 환경 변수로 지정된 파일 우선
		viper.SetConfigFile(cfgFile)
		// 확장자를 기반으로 타입 강제
		if ext := filepath.Ext(cfgFile); ext != "" {
			viper.SetConfigType(ext[1:]) // "json", "yaml" 등
		}
	} else {
		// 기본 파일명: server_config.(json|yaml)
		viper.SetConfigName("server_config")
		viper.AddConfigPath(".")
	}

	// 4) 파일 읽기
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			// 파일이 지정된 경우 경로 포함, 기본 위치일 때는 간단히 로그
			if cfgFile != "" {
				logger.Warnf("Config file %s not found, using defaults", cfgFile)
			} else {
				logger.Warnf("Config file not found in default locations, using defaults")
			}
		} else {
			// 실제 사용된 파일 경로가 빈 문자열일 수 있어, cfgFile 로 대체
			filePath := viper.ConfigFileUsed()
			if filePath == "" && cfgFile != "" {
				filePath = cfgFile
			}
			logger.Warnf("Error reading config file %s: %v. Using defaults.", filePath, err)
		}
	}
}

// LoadServerConfig returns ServerConfig populated from viper.
func LoadServerConfig() *ServerConfig {
	return &ServerConfig{
		MaxRecvMsgSize:       viper.GetInt("max_recv_msg_size"),
		MaxSendMsgSize:       viper.GetInt("max_send_msg_size"),
		MaxConcurrentStreams: uint32(viper.GetUint("max_concurrent_streams")),
	}
}
