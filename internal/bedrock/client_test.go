package bedrock

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awssdkbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awssdkbedrockruntime "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stratum/gateway/internal/config"
)

type stubBedrockClient struct{}

func (stubBedrockClient) ListFoundationModels(ctx context.Context, params *awssdkbedrock.ListFoundationModelsInput, optFns ...func(*awssdkbedrock.Options)) (*awssdkbedrock.ListFoundationModelsOutput, error) {
	return &awssdkbedrock.ListFoundationModelsOutput{}, nil
}

type stubRuntimeClient struct{}

func (stubRuntimeClient) Converse(ctx context.Context, params *awssdkbedrockruntime.ConverseInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseOutput, error) {
	return &awssdkbedrockruntime.ConverseOutput{}, nil
}

func (stubRuntimeClient) ConverseStream(ctx context.Context, params *awssdkbedrockruntime.ConverseStreamInput, optFns ...func(*awssdkbedrockruntime.Options)) (*awssdkbedrockruntime.ConverseStreamOutput, error) {
	return &awssdkbedrockruntime.ConverseStreamOutput{}, nil
}

func TestNewClient_ConfiguresRegionAndRetryer(t *testing.T) {
	oldLoad := loadAWSDefaultConfig
	oldNewBedrock := newBedrockAPIClient
	oldNewRuntime := newRuntimeAPIClient
	defer func() {
		loadAWSDefaultConfig = oldLoad
		newBedrockAPIClient = oldNewBedrock
		newRuntimeAPIClient = oldNewRuntime
	}()

	var captured awsconfig.LoadOptions
	loadAWSDefaultConfig = func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		var opts awsconfig.LoadOptions
		for _, fn := range optFns {
			if err := fn(&opts); err != nil {
				return aws.Config{}, err
			}
		}
		captured = opts
		return aws.Config{Region: opts.Region}, nil
	}

	bedrockStub := stubBedrockClient{}
	runtimeStub := stubRuntimeClient{}
	newBedrockAPIClient = func(cfg aws.Config) bedrockAPI { return bedrockStub }
	newRuntimeAPIClient = func(cfg aws.Config) bedrockRuntimeAPI { return runtimeStub }

	cfg := &config.Config{AWSRegion: "us-west-2"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
	if captured.Region != "us-west-2" {
		t.Fatalf("expected region us-west-2, got %q", captured.Region)
	}
	if captured.Retryer == nil {
		t.Fatal("expected retryer to be configured")
	}
	if _, ok := captured.Retryer().(*retry.AdaptiveMode); !ok {
		t.Fatalf("expected adaptive retry mode")
	}
}

func TestNewClient_UsesStaticCredentialsWhenProvided(t *testing.T) {
	oldLoad := loadAWSDefaultConfig
	oldNewBedrock := newBedrockAPIClient
	oldNewRuntime := newRuntimeAPIClient
	defer func() {
		loadAWSDefaultConfig = oldLoad
		newBedrockAPIClient = oldNewBedrock
		newRuntimeAPIClient = oldNewRuntime
	}()

	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA_TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET_TEST")

	var captured awsconfig.LoadOptions
	loadAWSDefaultConfig = func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		var opts awsconfig.LoadOptions
		for _, fn := range optFns {
			if err := fn(&opts); err != nil {
				return aws.Config{}, err
			}
		}
		captured = opts
		return aws.Config{Region: opts.Region}, nil
	}
	newBedrockAPIClient = func(cfg aws.Config) bedrockAPI { return stubBedrockClient{} }
	newRuntimeAPIClient = func(cfg aws.Config) bedrockRuntimeAPI { return stubRuntimeClient{} }

	if _, err := NewClient(&config.Config{AWSRegion: "us-east-1"}); err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if captured.Credentials == nil {
		t.Fatal("expected static credentials provider")
	}

	creds, err := captured.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("retrieve credentials: %v", err)
	}
	if creds.AccessKeyID != "AKIA_TEST" || creds.SecretAccessKey != "SECRET_TEST" {
		t.Fatalf("unexpected static credentials: %+v", creds)
	}
}

func TestNewClient_DoesNotSetStaticCredentialsWhenIncomplete(t *testing.T) {
	oldLoad := loadAWSDefaultConfig
	oldNewBedrock := newBedrockAPIClient
	oldNewRuntime := newRuntimeAPIClient
	defer func() {
		loadAWSDefaultConfig = oldLoad
		newBedrockAPIClient = oldNewBedrock
		newRuntimeAPIClient = oldNewRuntime
	}()

	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA_TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	var captured awsconfig.LoadOptions
	loadAWSDefaultConfig = func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		var opts awsconfig.LoadOptions
		for _, fn := range optFns {
			if err := fn(&opts); err != nil {
				return aws.Config{}, err
			}
		}
		captured = opts
		return aws.Config{Region: opts.Region}, nil
	}
	newBedrockAPIClient = func(cfg aws.Config) bedrockAPI { return stubBedrockClient{} }
	newRuntimeAPIClient = func(cfg aws.Config) bedrockRuntimeAPI { return stubRuntimeClient{} }

	if _, err := NewClient(&config.Config{AWSRegion: "us-east-1"}); err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if captured.Credentials != nil {
		t.Fatal("did not expect static credentials provider")
	}
}

func TestNewClient_LoadConfigError(t *testing.T) {
	oldLoad := loadAWSDefaultConfig
	defer func() { loadAWSDefaultConfig = oldLoad }()

	loadAWSDefaultConfig = func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return aws.Config{}, errors.New("load failed")
	}

	_, err := NewClient(&config.Config{AWSRegion: "us-east-1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to load AWS config") {
		t.Fatalf("unexpected error: %v", err)
	}
}
