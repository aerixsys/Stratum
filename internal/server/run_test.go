package server

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	awssdkbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awssdkbedrockruntime "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	awssdkbrtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	gatewaybedrock "github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/config"
)

type stubBedrockAPI struct{}

func (s *stubBedrockAPI) ListFoundationModels(ctx context.Context, params *awssdkbedrock.ListFoundationModelsInput, optFns ...func(*awssdkbedrock.Options)) (*awssdkbedrock.ListFoundationModelsOutput, error) {
	return &awssdkbedrock.ListFoundationModelsOutput{}, nil
}

type stubRuntimeAPI struct{}

func (s *stubRuntimeAPI) Converse(ctx context.Context, params *awssdkbedrockruntime.ConverseInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseOutput, error) {
	return &awssdkbedrockruntime.ConverseOutput{
		StopReason: awssdkbrtypes.StopReasonEndTurn,
	}, nil
}

func (s *stubRuntimeAPI) ConverseStream(ctx context.Context, params *awssdkbedrockruntime.ConverseStreamInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseStreamOutput, error) {
	return nil, errors.New("not implemented")
}

func TestRun_GracefulShutdown(t *testing.T) {
	oldNewClient := newBedrockClient
	oldNotify := notifyContext
	defer func() {
		newBedrockClient = oldNewClient
		notifyContext = oldNotify
	}()

	newBedrockClient = func(cfg *config.Config) (*gatewaybedrock.Client, error) {
		return &gatewaybedrock.Client{
			Bedrock:        &stubBedrockAPI{},
			BedrockRuntime: &stubRuntimeAPI{},
			Config:         cfg,
		}, nil
	}
	notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		return ctx, cancel
	}

	cfg := &config.Config{
		Port:                "0",
		LogLevel:            "error",
		APIKey:              "sk-test",
		AWSRegion:           "us-east-1",
		MaxRequestBodyBytes: 1024 * 1024,
		ModelPolicyPath:     testModelPolicyPath(t),
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_BedrockClientError(t *testing.T) {
	oldNewClient := newBedrockClient
	defer func() { newBedrockClient = oldNewClient }()

	newBedrockClient = func(cfg *config.Config) (*gatewaybedrock.Client, error) {
		return nil, errors.New("boom")
	}

	cfg := &config.Config{
		Port:                "0",
		LogLevel:            "error",
		APIKey:              "sk-test",
		AWSRegion:           "us-east-1",
		MaxRequestBodyBytes: 1024 * 1024,
		ModelPolicyPath:     testModelPolicyPath(t),
	}

	err := Run(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bedrock client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_ServerListenError(t *testing.T) {
	oldNewClient := newBedrockClient
	oldNotify := notifyContext
	defer func() {
		newBedrockClient = oldNewClient
		notifyContext = oldNotify
	}()

	newBedrockClient = func(cfg *config.Config) (*gatewaybedrock.Client, error) {
		return &gatewaybedrock.Client{
			Bedrock:        &stubBedrockAPI{},
			BedrockRuntime: &stubRuntimeAPI{},
			Config:         cfg,
		}, nil
	}
	notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{
		Port:                strconv.Itoa(port),
		LogLevel:            "error",
		APIKey:              "sk-test",
		AWSRegion:           "us-east-1",
		MaxRequestBodyBytes: 1024 * 1024,
		ModelPolicyPath:     testModelPolicyPath(t),
	}

	err = Run(cfg)
	if err == nil {
		t.Fatal("expected listen error")
	}
	if !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

var _ interface {
	Converse(ctx context.Context, params *awssdkbedrockruntime.ConverseInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *awssdkbedrockruntime.ConverseStreamInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseStreamOutput, error)
} = (*stubRuntimeAPI)(nil)

var _ interface {
	ListFoundationModels(ctx context.Context, params *awssdkbedrock.ListFoundationModelsInput, optFns ...func(*awssdkbedrock.Options)) (*awssdkbedrock.ListFoundationModelsOutput, error)
} = (*stubBedrockAPI)(nil)

func testModelPolicyPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(root, "config", "model-policy.yaml")
}
