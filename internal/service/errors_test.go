package service

import (
	"errors"
	"strings"
	"testing"
)

func TestServiceErrorHelpers(t *testing.T) {
	cause := errors.New("boom")
	err := internal("failed", cause)
	if got := err.Error(); !strings.Contains(got, "failed") || !strings.Contains(got, "boom") {
		t.Fatalf("unexpected error string: %s", got)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected wrapped cause")
	}
	if err.Unwrap() != cause {
		t.Fatalf("unexpected unwrap value")
	}
}
