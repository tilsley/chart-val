// Package logger provides structured logging with colored output.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// New creates a structured logger writing to stdout at the given level.
// Uses colored text format by default, JSON if LOG_FORMAT=json env var is set.
// Colors can be disabled by setting NO_COLOR=1 or LOG_COLOR=false.
func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	var handler slog.Handler
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: l,
		})
	} else {
		// Use colored text handler
		useColor := shouldUseColor()
		handler = &coloredTextHandler{
			w:        os.Stdout,
			level:    l,
			useColor: useColor,
		}
	}

	return slog.New(handler)
}

// shouldUseColor determines if colored output should be used.
func shouldUseColor() bool {
	// Respect NO_COLOR env var (https://no-color.org/)
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// Respect LOG_COLOR env var
	if logColor := strings.ToLower(os.Getenv("LOG_COLOR")); logColor == "false" || logColor == "0" {
		return false
	}
	return true
}

// coloredTextHandler is a custom slog.Handler that outputs colored text logs.
type coloredTextHandler struct {
	w        io.Writer
	level    slog.Level
	useColor bool
	attrs    []slog.Attr
	groups   []string
}

func (h *coloredTextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *coloredTextHandler) Handle(_ context.Context, r slog.Record) error {
	var buf strings.Builder

	// Time (gray)
	if h.useColor {
		buf.WriteString(colorGray)
	}
	buf.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	if h.useColor {
		buf.WriteString(colorReset)
	}
	buf.WriteString(" ")

	// Level (colored based on severity)
	levelStr := r.Level.String()
	if h.useColor {
		switch r.Level {
		case slog.LevelDebug:
			buf.WriteString(colorCyan)
			levelStr = "DEBUG"
		case slog.LevelInfo:
			buf.WriteString(colorBlue)
			levelStr = "INFO "
		case slog.LevelWarn:
			buf.WriteString(colorYellow)
			levelStr = "WARN "
		case slog.LevelError:
			buf.WriteString(colorRed + colorBold)
			levelStr = "ERROR"
		}
	}
	buf.WriteString(levelStr)
	if h.useColor {
		buf.WriteString(colorReset)
	}
	buf.WriteString(" ")

	// Message
	buf.WriteString(r.Message)

	// Attributes
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteString(" ")
		if h.useColor {
			buf.WriteString(colorGray)
		}
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(a.Value.String())
		if h.useColor {
			buf.WriteString(colorReset)
		}
		return true
	})

	// Handler-level attributes
	for _, a := range h.attrs {
		buf.WriteString(" ")
		if h.useColor {
			buf.WriteString(colorGray)
		}
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(a.Value.String())
		if h.useColor {
			buf.WriteString(colorReset)
		}
	}

	buf.WriteString("\n")
	_, err := h.w.Write([]byte(buf.String()))
	return err
}

func (h *coloredTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &coloredTextHandler{
		w:        h.w,
		level:    h.level,
		useColor: h.useColor,
		attrs:    newAttrs,
		groups:   h.groups,
	}
}

func (h *coloredTextHandler) WithGroup(name string) slog.Handler {
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name
	return &coloredTextHandler{
		w:        h.w,
		level:    h.level,
		useColor: h.useColor,
		attrs:    h.attrs,
		groups:   newGroups,
	}
}

// init ensures we print a notice if colors are disabled
//
//nolint:gochecknoinits // Required to print color status on startup
func init() {
	if !shouldUseColor() && os.Getenv("LOG_FORMAT") != "json" {
		fmt.Fprintf(os.Stderr, "# Colored logs disabled (NO_COLOR or LOG_COLOR=false)\n")
	}
}
