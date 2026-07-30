// Package logging provides a structured zap-based logger for the backend,
// plus an async sink that mirrors error-level (and above) entries into the
// error_logs Postgres table without blocking the caller.
package logging

import (
	"context"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ErrorSink is implemented by anything that can persist an application
// error log entry (typically internal/repository/postgres.ErrorLogRepo).
type ErrorSink interface {
	Create(ctx context.Context, service, level, message string, metadata map[string]any) error
}

// sinkCore is a zapcore.Core wrapper that additionally forwards
// error-and-above entries to an ErrorSink via a bounded async queue, so
// logging never blocks request handling on a database write.
type sinkCore struct {
	zapcore.Core
	service string
	queue   chan logEntry
}

type logEntry struct {
	level   string
	message string
	fields  map[string]any
}

func newSinkCore(core zapcore.Core, service string, sink ErrorSink) *sinkCore {
	sc := &sinkCore{Core: core, service: service, queue: make(chan logEntry, 1024)}
	if sink != nil {
		go sc.run(sink)
	}
	return sc
}

func (c *sinkCore) run(sink ErrorSink) {
	for entry := range c.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = sink.Create(ctx, c.service, entry.level, entry.message, entry.fields)
		cancel()
	}
}

func (c *sinkCore) With(fields []zapcore.Field) zapcore.Core {
	return &sinkCore{Core: c.Core.With(fields), service: c.service, queue: c.queue}
}

func (c *sinkCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		ce = ce.AddCore(entry, c)
	}
	return ce
}

func (c *sinkCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if entry.Level >= zapcore.ErrorLevel {
		enc := zapcore.NewMapObjectEncoder()
		for _, f := range fields {
			f.AddTo(enc)
		}
		select {
		case c.queue <- logEntry{level: entry.Level.String(), message: entry.Message, fields: enc.Fields}:
		default:
			// Queue full: drop rather than block. The primary log output
			// (stdout) below still captures the entry.
		}
	}
	return c.Core.Write(entry, fields)
}

var (
	globalMu sync.Mutex
	sink     ErrorSink
)

// SetErrorSink installs the repository used to persist error-level logs.
// Safe to call once during startup after the DB connection is ready; log
// entries emitted before this call simply won't be persisted to Postgres
// (they still go to stdout).
func SetErrorSink(s ErrorSink) {
	globalMu.Lock()
	defer globalMu.Unlock()
	sink = s
}

// New builds a production-style zap.Logger. level is one of
// debug/info/warn/error. service tags every entry to make error_logs
// filterable per component (e.g. "backend-api", "backend-grpc").
func New(level, service string, env string) (*zap.Logger, error) {
	var zl zapcore.Level
	if err := zl.UnmarshalText([]byte(level)); err != nil {
		zl = zapcore.InfoLevel
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	var encoder zapcore.Encoder
	if env == "development" {
		encCfg = zap.NewDevelopmentEncoderConfig()
		encoder = zapcore.NewConsoleEncoder(encCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encCfg)
	}

	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(os.Stdout)), zl)
	wrapped := newSinkCore(core, service, sinkAdapter{})

	logger := zap.New(wrapped, zap.AddCaller(), zap.Fields(zap.String("service", service)))
	return logger, nil
}

// sinkAdapter defers to the package-level sink variable so New() can be
// called before SetErrorSink without capturing a nil.
type sinkAdapter struct{}

func (sinkAdapter) Create(ctx context.Context, service, level, message string, metadata map[string]any) error {
	globalMu.Lock()
	s := sink
	globalMu.Unlock()
	if s == nil {
		return nil
	}
	return s.Create(ctx, service, level, message, metadata)
}
