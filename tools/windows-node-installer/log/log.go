package log

import (
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// SetLogLevel sets the level of log for display in command line using zapcore.Levels ("debug", "info", "warn",
// "error", "dpanic", "panic", and "fatal") returns an error if input log level is not understandable.
func SetLogLevel(logLevel []byte) error {
	cfg := zap.NewDevelopmentConfig()
	// Building a logger wrapper.
	zapLog, err := cfg.Build()
	if err != nil {
		return err
	}
	logger.SetLogger(zapr.NewLogger(zapLog))
	return cfg.Level.UnmarshalText(logLevel)
}
