// Package logger provides the process-wide logging API used by croc.
//
// It preserves croc's established logging API while keeping the configured
// level in an atomic value, so clients and relay servers may adjust logging
// without racing concurrent log calls.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
)

const (
	levelTrace int32 = iota
	levelDebug
	levelInfo
	levelWarn
	levelError
)

const (
	red    = "\033[0;31;1m"
	yellow = "\033[0;33m"
	white  = "\033[0;37m"
	cyan   = "\033[0;36m"
	blue   = "\033[0;34;1m"
	end    = "\033[0m"
)

var standard = New()

// Logger is a concurrency-safe leveled logger.
type Logger struct {
	trace *log.Logger
	debug *log.Logger
	info  *log.Logger
	warn  *log.Logger
	error *log.Logger
	level atomic.Int32
}

// New returns a logger writing to stdout with trace logging enabled, unless
// LOGGER selects a different minimum level.
func New() *Logger {
	l := &Logger{
		trace: log.New(os.Stdout, "[trace]\t", log.Ltime|log.Lmicroseconds|log.Lshortfile),
		debug: log.New(os.Stdout, "[debug]\t", log.Ltime|log.Lshortfile),
		info:  log.New(os.Stdout, "[info]\t", log.Ldate|log.Ltime),
		warn:  log.New(os.Stdout, "[warn]\t", log.Ldate|log.Ltime),
		error: log.New(os.Stdout, "[error]\t", log.Ldate|log.Ltime|log.Lshortfile),
	}
	if runtime.GOOS == "linux" {
		l.trace.SetPrefix(blue + l.trace.Prefix() + end)
		l.debug.SetPrefix(cyan + l.debug.Prefix() + end)
		l.info.SetPrefix(white + l.info.Prefix() + end)
		l.warn.SetPrefix(yellow + l.warn.Prefix() + end)
		l.error.SetPrefix(red + l.error.Prefix() + end)
	}
	l.level.Store(levelTrace)
	if configured := configuredLevel(); configured != "" {
		l.setLevel(configured)
	}
	return l
}

func configuredLevel() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv("LOGGER")))
}

// SetOutput changes the destination for all severity levels.
func SetOutput(w io.Writer) { standard.SetOutput(w) }

// SetOutput changes the destination for all severity levels.
func (l *Logger) SetOutput(w io.Writer) {
	l.trace.SetOutput(w)
	l.debug.SetOutput(w)
	l.info.SetOutput(w)
	l.warn.SetOutput(w)
	l.error.SetOutput(w)
}

// SetLevel changes the process-wide minimum level. LOGGER takes precedence.
func SetLevel(level string) {
	if configuredLevel() != "" {
		return
	}
	standard.setLevel(level)
}

// SetLevel changes this logger's minimum level.
func (l *Logger) SetLevel(level string) { l.setLevel(level) }

func (l *Logger) setLevel(level string) {
	switch strings.TrimSpace(strings.ToLower(level)) {
	case "debug":
		l.level.Store(levelDebug)
	case "info":
		l.level.Store(levelInfo)
	case "warn":
		l.level.Store(levelWarn)
	case "error":
		l.level.Store(levelError)
	default:
		l.level.Store(levelTrace)
	}
}

// GetLevel returns the process-wide minimum level.
func GetLevel() string { return standard.GetLevel() }

// GetLevel returns this logger's minimum level.
func (l *Logger) GetLevel() string {
	switch l.level.Load() {
	case levelDebug:
		return "debug"
	case levelInfo:
		return "info"
	case levelWarn:
		return "warn"
	case levelError:
		return "error"
	default:
		return "trace"
	}
}

func Tracef(format string, values ...any) { standard.Tracef(format, values...) }
func Debugf(format string, values ...any) { standard.Debugf(format, values...) }
func Infof(format string, values ...any)  { standard.Infof(format, values...) }
func Warnf(format string, values ...any)  { standard.Warnf(format, values...) }
func Errorf(format string, values ...any) { standard.Errorf(format, values...) }

func Trace(values ...any) { Tracef("%s", fmt.Sprint(values...)) }
func Debug(values ...any) { Debugf("%s", fmt.Sprint(values...)) }
func Info(values ...any)  { Infof("%s", fmt.Sprint(values...)) }
func Warn(values ...any)  { Warnf("%s", fmt.Sprint(values...)) }
func Error(values ...any) { Errorf("%s", fmt.Sprint(values...)) }

func (l *Logger) Tracef(format string, values ...any) {
	if l.level.Load() <= levelTrace {
		_ = l.trace.Output(3, fmt.Sprintf(format, values...))
	}
}

func (l *Logger) Debugf(format string, values ...any) {
	if l.level.Load() <= levelDebug {
		_ = l.debug.Output(3, fmt.Sprintf(format, values...))
	}
}

func (l *Logger) Infof(format string, values ...any) {
	if l.level.Load() <= levelInfo {
		_ = l.info.Output(3, fmt.Sprintf(format, values...))
	}
}

func (l *Logger) Warnf(format string, values ...any) {
	if l.level.Load() <= levelWarn {
		_ = l.warn.Output(3, fmt.Sprintf(format, values...))
	}
}

func (l *Logger) Errorf(format string, values ...any) {
	if l.level.Load() <= levelError {
		_ = l.error.Output(3, fmt.Sprintf(format, values...))
	}
}

func (l *Logger) Trace(values ...any) { l.Tracef("%s", fmt.Sprint(values...)) }
func (l *Logger) Debug(values ...any) { l.Debugf("%s", fmt.Sprint(values...)) }
func (l *Logger) Info(values ...any)  { l.Infof("%s", fmt.Sprint(values...)) }
func (l *Logger) Warn(values ...any)  { l.Warnf("%s", fmt.Sprint(values...)) }
func (l *Logger) Error(values ...any) { l.Errorf("%s", fmt.Sprint(values...)) }
