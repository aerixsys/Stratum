package logging

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

type level int32

const (
	levelDebug level = iota
	levelInfo
	levelWarn
	levelError
)

// ANSI color codes.
const (
	ansiReset  = "\033[0m"
	ansiGray   = "\033[90m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
)

// Icons for log categories.
const (
	iconInfo    = "✦"
	iconAccess  = "←"
	iconStream  = "⚡"
	iconWarning = "⚠"
	iconError   = "✕"
	iconOK      = "✓"
)

var currentLevel atomic.Int32
var isTTY atomic.Int32 // 0=undetected, 1=tty, 2=not-tty
var outputMu sync.RWMutex
var outputWriter io.Writer = os.Stdout

func init() {
	currentLevel.Store(int32(levelInfo))
	detectTTY()
}

func detectTTY() {
	_, err := unix.IoctlGetTermios(int(os.Stdout.Fd()), unix.TCGETS)
	if err == nil {
		isTTY.Store(1)
		return
	}
	isTTY.Store(2)
}

func ttyMode() bool {
	return isTTY.Load() == 1
}

func Configure(levelRaw string) error {
	lvl, err := parseLevel(levelRaw)
	if err != nil {
		return err
	}
	currentLevel.Store(int32(lvl))
	return nil
}

func IsDebug() bool {
	return level(currentLevel.Load()) <= levelDebug
}

func Debugf(message string, args ...any) {
	logf(levelDebug, message, args...)
}

func Infof(message string, args ...any) {
	logf(levelInfo, message, args...)
}

func Warnf(message string, args ...any) {
	logf(levelWarn, message, args...)
}

func Errorf(message string, args ...any) {
	logf(levelError, message, args...)
}

func DebugWithFields(message string, fields map[string]any) {
	logWithFields(levelDebug, message, fields)
}

func InfoWithFields(message string, fields map[string]any) {
	logWithFields(levelInfo, message, fields)
}

func WarnWithFields(message string, fields map[string]any) {
	logWithFields(levelWarn, message, fields)
}

func ErrorWithFields(message string, fields map[string]any) {
	logWithFields(levelError, message, fields)
}

// AccessLog writes a compact access-log line.
// TTY: "00:20:35 INFO  ← GET /v1/chat/completions 200 268ms [::1]"
// Non-TTY: "timestamp [INFO]  ← GET /v1/chat/completions 200 268ms ip=::1"
func AccessLog(method, path string, status int, latencyMs int64, clientIP string, errorCount int, errors string) {
	if levelInfo < level(currentLevel.Load()) {
		return
	}

	writer := getWriter()
	if ttyMode() {
		ts := time.Now().UTC().Format("15:04:05")
		lvlStr := colorLevel(levelInfo)

		statusColor := ansiGreen
		if status >= 400 && status < 500 {
			statusColor = ansiYellow
		} else if status >= 500 {
			statusColor = ansiRed
		}

		line := fmt.Sprintf("%s%s%s %s %s%s%s %s %s %s%d%s %s %s[%s]%s",
			ansiDim, ts, ansiReset,
			lvlStr,
			ansiCyan, iconAccess, ansiReset,
			method,
			path,
			statusColor, status, ansiReset,
			dimLatency(status, latencyMs),
			ansiDim, clientIP, ansiReset,
		)
		if errorCount > 0 {
			line += fmt.Sprintf(" %s%s errors=%d%s", ansiRed, iconError, errorCount, ansiReset)
		}
		_, _ = fmt.Fprintln(writer, line)
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	line := fmt.Sprintf("%s [INFO]  %s %s %s %d %dms ip=%s",
		ts,
		iconAccess,
		method,
		path,
		status,
		latencyMs,
		clientIP,
	)
	if errorCount > 0 {
		line += fmt.Sprintf(" errors=%d err=%s", errorCount, errors)
	}
	_, _ = fmt.Fprintln(writer, line)
}

// StreamLog writes a compact stream event line.
// TTY: "00:20:47 DEBUG ⚡ stream_done model=nova tokens=0 1120ms"
// Non-TTY: "timestamp [DEBUG] ⚡ stream_done model=nova tokens=0 1120ms"
func StreamLog(event, model string, tokens, durationMs int64, extra map[string]any) {
	if levelDebug < level(currentLevel.Load()) {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	if tokens < 0 {
		tokens = 0
	}
	if durationMs < 0 {
		durationMs = 0
	}

	extraStr := formatFieldsCompact(extra)
	writer := getWriter()
	if ttyMode() {
		ts := time.Now().UTC().Format("15:04:05")
		lvlStr := colorLevel(levelDebug)
		line := fmt.Sprintf("%s%s%s %s %s%s%s %s%s%s model=%s%s%s tokens=%d %s%dms%s",
			ansiDim, ts, ansiReset,
			lvlStr,
			ansiCyan, iconStream, ansiReset,
			ansiDim, strings.TrimSpace(event), ansiReset,
			ansiBold, model, ansiReset,
			tokens,
			ansiDim, durationMs, ansiReset,
		)
		if extraStr != "" {
			line += " " + ansiDim + extraStr + ansiReset
		}
		_, _ = fmt.Fprintln(writer, line)
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	line := fmt.Sprintf("%s [DEBUG] %s %s model=%s tokens=%d %dms",
		ts,
		iconStream,
		strings.TrimSpace(event),
		model,
		tokens,
		durationMs,
	)
	if extraStr != "" {
		line += " " + extraStr
	}
	_, _ = fmt.Fprintln(writer, line)
}

// InferenceDone logs a final inference outcome.
func InferenceDone(model string, stream bool, status string, durationMs int64, totalTokens int) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "ok" {
		status = "error"
	}
	if durationMs < 0 {
		durationMs = 0
	}
	if totalTokens < 0 {
		totalTokens = 0
	}

	writer := getWriter()
	if ttyMode() {
		ts := time.Now().UTC().Format("15:04:05")
		var icon, statusColor string
		if status == "ok" {
			icon = ansiGreen + iconOK + ansiReset
			statusColor = ansiGreen
		} else {
			icon = ansiRed + iconError + ansiReset
			statusColor = ansiRed
		}
		lvl := colorLevel(levelInfo)
		streamTag := "non-stream"
		if stream {
			streamTag = "stream"
		}
		line := fmt.Sprintf("%s%s%s %s %s inference_done model=%s%s%s %s %s%s%s %s%dms%s",
			ansiDim, ts, ansiReset,
			lvl,
			icon,
			ansiBold, model, ansiReset,
			streamTag,
			statusColor, status, ansiReset,
			ansiDim, durationMs, ansiReset,
		)
		if totalTokens > 0 {
			line += fmt.Sprintf(" tokens=%d", totalTokens)
		}
		_, _ = fmt.Fprintln(writer, line)
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	line := fmt.Sprintf("%s [INFO]  inference_done model=%s stream=%t status=%s duration_ms=%d",
		ts,
		model,
		stream,
		status,
		durationMs,
	)
	if totalTokens > 0 {
		line += fmt.Sprintf(" tokens=%d", totalTokens)
	}
	_, _ = fmt.Fprintln(writer, line)
}

// Banner writes a startup banner block.
func Banner(lines []string) {
	writer := getWriter()
	width := 40
	border := strings.Repeat("━", width)

	if ttyMode() {
		_, _ = fmt.Fprintf(writer, "\n%s%s%s\n", ansiCyan, border, ansiReset)
		for _, line := range lines {
			_, _ = fmt.Fprintf(writer, "  %s\n", line)
		}
		_, _ = fmt.Fprintf(writer, "%s%s%s\n\n", ansiCyan, border, ansiReset)
		return
	}

	_, _ = fmt.Fprintf(writer, "\n%s\n", border)
	for _, line := range lines {
		_, _ = fmt.Fprintf(writer, "  %s\n", line)
	}
	_, _ = fmt.Fprintf(writer, "%s\n\n", border)
}

// SetOutput overrides the output writer (used in tests).
func SetOutput(w io.Writer) {
	outputMu.Lock()
	defer outputMu.Unlock()
	if w == nil {
		outputWriter = os.Stdout
		return
	}
	outputWriter = w
}

// SetTTYMode forces TTY mode on/off (used in tests).
func SetTTYMode(enabled bool) {
	if enabled {
		isTTY.Store(1)
		return
	}
	isTTY.Store(2)
}

func logf(lvl level, message string, args ...any) {
	if lvl < level(currentLevel.Load()) {
		return
	}
	msg := strings.TrimSpace(fmt.Sprintf(message, args...))
	if msg == "" {
		return
	}
	writeLine(lvl, msg)
}

func logWithFields(lvl level, message string, fields map[string]any) {
	if lvl < level(currentLevel.Load()) {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	if len(fields) > 0 {
		msg += " " + formatFields(fields)
	}
	writeLine(lvl, msg)
}

func writeLine(lvl level, msg string) {
	writer := getWriter()
	if ttyMode() {
		ts := time.Now().UTC().Format("15:04:05")
		icon := levelIcon(lvl)
		clr := levelColor(lvl)
		_, _ = fmt.Fprintf(writer, "%s%s%s %s %s%s%s %s\n",
			ansiDim, ts, ansiReset,
			colorLevel(lvl),
			clr, icon, ansiReset,
			msg,
		)
		return
	}
	_, _ = fmt.Fprintf(writer, "%s [%s] %s\n",
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.ToUpper(paddedLevel(lvl)),
		msg,
	)
}

func colorLevel(lvl level) string {
	switch lvl {
	case levelDebug:
		return ansiGray + "DEBUG" + ansiReset
	case levelInfo:
		return ansiCyan + "INFO " + ansiReset
	case levelWarn:
		return ansiYellow + "WARN " + ansiReset
	case levelError:
		return ansiRed + ansiBold + "ERROR" + ansiReset
	default:
		return ansiCyan + "INFO " + ansiReset
	}
}

func paddedLevel(lvl level) string {
	switch lvl {
	case levelDebug:
		return "DEBUG"
	case levelInfo:
		return "INFO "
	case levelWarn:
		return "WARN "
	case levelError:
		return "ERROR"
	default:
		return "INFO "
	}
}

func levelIcon(lvl level) string {
	switch lvl {
	case levelWarn:
		return iconWarning
	case levelError:
		return iconError
	default:
		return iconInfo
	}
}

func levelColor(lvl level) string {
	switch lvl {
	case levelDebug:
		return ansiGray
	case levelInfo:
		return ansiCyan
	case levelWarn:
		return ansiYellow
	case levelError:
		return ansiRed
	default:
		return ansiCyan
	}
}

func dimLatency(status int, latencyMs int64) string {
	s := fmt.Sprintf("%dms", latencyMs)
	if status < 400 && latencyMs < 1000 {
		return ansiDim + s + ansiReset
	}
	if latencyMs >= 3000 {
		return ansiYellow + s + ansiReset
	}
	return s
}

func getWriter() io.Writer {
	outputMu.RLock()
	defer outputMu.RUnlock()
	return outputWriter
}

func formatFieldsCompact(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", key, value))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func formatFields(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, fields[key]))
	}
	return strings.Join(parts, " ")
}

func parseLevel(raw string) (level, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return levelInfo, nil
	}
	switch raw {
	case "debug":
		return levelDebug, nil
	case "info":
		return levelInfo, nil
	case "warn", "warning":
		return levelWarn, nil
	case "error":
		return levelError, nil
	default:
		return levelInfo, fmt.Errorf("invalid LOG_LEVEL %q", raw)
	}
}
