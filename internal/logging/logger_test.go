package logging

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func withTestLogger(t *testing.T, lvl string, tty bool) *bytes.Buffer {
	t.Helper()
	if err := Configure(lvl); err != nil {
		t.Fatalf("configure: %v", err)
	}
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetTTYMode(tty)
	t.Cleanup(func() {
		SetOutput(nil)
		SetTTYMode(false)
		_ = Configure("info")
	})
	return buf
}

func TestConfigure_InvalidLevel(t *testing.T) {
	if err := Configure("verbose"); err == nil {
		t.Fatal("expected invalid LOG_LEVEL error")
	}
}

func TestInfof_NonTTYFormat(t *testing.T) {
	buf := withTestLogger(t, "info", false)
	Infof("hello %s", "world")

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "[INFO ]") {
		t.Fatalf("expected [INFO ] prefix, got %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("expected message content, got %q", out)
	}
	if matched := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`).MatchString(out); !matched {
		t.Fatalf("expected RFC3339 timestamp prefix, got %q", out)
	}
}

func TestWithFields_SortedKeys(t *testing.T) {
	buf := withTestLogger(t, "info", false)
	InfoWithFields("event", map[string]any{
		"z": 3,
		"a": 1,
		"m": 2,
	})

	out := buf.String()
	ai := strings.Index(out, "a=1")
	mi := strings.Index(out, "m=2")
	zi := strings.Index(out, "z=3")
	if ai == -1 || mi == -1 || zi == -1 {
		t.Fatalf("expected all fields in output, got %q", out)
	}
	if !(ai < mi && mi < zi) {
		t.Fatalf("expected sorted fields order a,m,z, got %q", out)
	}
}

func TestAccessLog_DebugLevelAndErrors(t *testing.T) {
	buf := withTestLogger(t, "debug", false)
	AccessLog("GET", "/v1/models", 500, 1234, "203.0.113.7", 2, "boom")

	out := buf.String()
	if !strings.Contains(out, "GET /v1/models 500 1234ms ip=203.0.113.7") {
		t.Fatalf("unexpected access log output: %q", out)
	}
	if !strings.Contains(out, "errors=2 err=boom") {
		t.Fatalf("expected error details in access log: %q", out)
	}
}

func TestStreamLog_OnlyWhenDebug(t *testing.T) {
	bufInfo := withTestLogger(t, "info", false)
	StreamLog("stream_done", "amazon.nova-micro-v1:0", 5, 400, nil)
	if bufInfo.Len() != 0 {
		t.Fatalf("expected no stream logs at info level, got %q", bufInfo.String())
	}

	bufDebug := withTestLogger(t, "debug", false)
	StreamLog("stream_done", "amazon.nova-micro-v1:0", 5, 400, map[string]any{"stage": "upstream"})
	out := bufDebug.String()
	if !strings.Contains(out, "[DEBUG]") {
		t.Fatalf("expected debug level line, got %q", out)
	}
	if !strings.Contains(out, "stream_done") || !strings.Contains(out, "model=amazon.nova-micro-v1:0") {
		t.Fatalf("expected stream details, got %q", out)
	}
	if !strings.Contains(out, "tokens=5 400ms") {
		t.Fatalf("expected stream token/duration details, got %q", out)
	}
}

func TestBanner_WritesContent(t *testing.T) {
	buf := withTestLogger(t, "info", false)
	Banner([]string{"Stratum Gateway", "Port: 8000"})
	out := buf.String()
	if !strings.Contains(out, "Stratum Gateway") {
		t.Fatalf("expected banner content, got %q", out)
	}
	if !strings.Contains(out, "Port: 8000") {
		t.Fatalf("expected banner line, got %q", out)
	}
}

func TestTTYOutputContainsANSI(t *testing.T) {
	buf := withTestLogger(t, "info", true)
	Infof("tty line")
	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escape sequence in tty output, got %q", out)
	}
}
