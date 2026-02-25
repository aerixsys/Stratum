package bedrock

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stratum/gateway/internal/config"
)

type bedrockAPI interface {
	ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
	ListInferenceProfiles(ctx context.Context, params *bedrock.ListInferenceProfilesInput, optFns ...func(*bedrock.Options)) (*bedrock.ListInferenceProfilesOutput, error)
}

type bedrockRuntimeAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

// Client wraps AWS Bedrock SDK clients.
type Client struct {
	Bedrock        bedrockAPI
	BedrockRuntime bedrockRuntimeAPI
	Config         *config.Config
}

// NewClient creates Bedrock clients from config.
func NewClient(cfg *config.Config) (*Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.AWSRegion),
		// Adaptive retry with 5 max attempts for throttling resilience
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
				o.StandardOptions = append(o.StandardOptions, func(so *retry.StandardOptions) {
					so.MaxAttempts = 5
				})
			})
		}),
	}

	// Use explicit credentials if provided, otherwise fall back to SDK chain.
	ak := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	sk := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if ak != "" && sk != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(ak, sk, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Client{
		Bedrock:        bedrock.NewFromConfig(awsCfg),
		BedrockRuntime: bedrockruntime.NewFromConfig(awsCfg),
		Config:         cfg,
	}, nil
}
