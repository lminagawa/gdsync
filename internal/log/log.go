package log

import (
	"log/slog"
	"os"
	"sync/atomic"
)

var current atomic.Pointer[slog.Logger]

func init() {
	l := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	current.Store(l)
}

func SetVerbose(v bool) {
	level := slog.LevelInfo
	if v {
		level = slog.LevelDebug
	}
	current.Store(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func Info(msg string, args ...any)  { current.Load().Info(msg, args...) }
func Debug(msg string, args ...any) { current.Load().Debug(msg, args...) }
func Warn(msg string, args ...any)  { current.Load().Warn(msg, args...) }
func Error(msg string, args ...any) { current.Load().Error(msg, args...) }
