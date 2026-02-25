#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-reports}"

# Load local env (AWS_REGION/credentials) when present.
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

TMP_GO="$(mktemp /tmp/export_aws_catalog_XXXX.go)"
cleanup() {
  rm -f "${TMP_GO}"
}
trap cleanup EXIT

cat > "${TMP_GO}" <<'EOF'
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes"
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func supportsOnDemand(fm bedrocktypes.FoundationModelSummary) bool {
	for _, t := range fm.InferenceTypesSupported {
		if t == bedrocktypes.InferenceTypeOnDemand {
			return true
		}
	}
	return false
}

func supportsTextOutput(fm bedrocktypes.FoundationModelSummary) bool {
	for _, m := range fm.OutputModalities {
		if m == bedrocktypes.ModelModalityText {
			return true
		}
	}
	return false
}

func hasDisallowedOutputModalities(fm bedrocktypes.FoundationModelSummary) bool {
	for _, m := range fm.OutputModalities {
		if m != bedrocktypes.ModelModalityText {
			return true
		}
	}
	return false
}

func modelIDFromFoundationARN(modelARN string) string {
	const marker = "foundation-model/"
	idx := strings.Index(modelARN, marker)
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(modelARN[idx+len(marker):])
}

func profileSupportsTextOnlyOutputs(profile bedrocktypes.InferenceProfileSummary, foundations map[string]bool) bool {
	if len(profile.Models) == 0 {
		return false
	}
	for _, m := range profile.Models {
		modelID := modelIDFromFoundationARN(deref(m.ModelArn))
		if modelID == "" {
			return false
		}
		if !foundations[modelID] {
			return false
		}
	}
	return true
}

func hasModality(mods []bedrocktypes.ModelModality, target bedrocktypes.ModelModality) bool {
	for _, m := range mods {
		if m == target {
			return true
		}
	}
	return false
}

func joinModalities(mods []bedrocktypes.ModelModality) string {
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func joinInferenceTypes(types []bedrocktypes.InferenceType) string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func joinCustomizations(values []bedrocktypes.ModelCustomization) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func providerFromID(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return parts[0]
	}
	switch parts[0] {
	case "us", "global", "eu", "ap", "apac":
		return parts[1]
	default:
		return parts[0]
	}
}

func main() {
	var outDir string
	flag.StringVar(&outDir, "out-dir", "reports", "Output directory")
	flag.Parse()

	region := strings.TrimSpace(os.Getenv("AWS_REGION"))
	if region == "" {
		region = "us-east-1"
	}
	enableCross := envBool("ENABLE_CROSS_REGION_INFERENCE", true)
	enableApp := envBool("ENABLE_APP_INFERENCE_PROFILES", false)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load aws config: %v\n", err)
		os.Exit(1)
	}

	client := bedrock.NewFromConfig(awsCfg)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", outDir, err)
		os.Exit(1)
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	foundationPath := fmt.Sprintf("%s/aws-foundation-catalog-%s.csv", outDir, ts)
	profilesPath := fmt.Sprintf("%s/aws-inference-profiles-%s.csv", outDir, ts)
	discoveredPath := fmt.Sprintf("%s/aws-discovered-equivalent-%s.txt", outDir, ts)
	summaryPath := fmt.Sprintf("%s/aws-catalog-summary-%s.txt", outDir, ts)

	fmOut, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListFoundationModels: %v\n", err)
		os.Exit(1)
	}

	foundationFile, err := os.Create(foundationPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", foundationPath, err)
		os.Exit(1)
	}
	defer foundationFile.Close()

	fw := csv.NewWriter(foundationFile)
	defer fw.Flush()

	_ = fw.Write([]string{
		"model_id",
		"provider",
		"model_name",
		"lifecycle_status",
		"inference_types",
		"input_modalities",
		"output_modalities",
		"response_streaming_supported",
		"customizations_supported",
		"supports_on_demand",
		"supports_text_output",
		"supports_image_input",
		"supports_image_output",
		"chat_like_candidate",
	})

	seen := map[string]bool{}
	foundationsForProfiles := map[string]bool{}
	discovered := []string{}
	providerCounts := map[string]int{}

	foundationRaw := 0
	chatLike := 0
	imageIn := 0
	imageOut := 0

	for _, fm := range fmOut.ModelSummaries {
		id := strings.TrimSpace(deref(fm.ModelId))
		if id == "" {
			continue
		}
		foundationRaw++

		provider := providerFromID(id)
		providerCounts[provider]++

		onDemand := supportsOnDemand(fm)
		textOut := hasModality(fm.OutputModalities, bedrocktypes.ModelModalityText)
		hasImageIn := hasModality(fm.InputModalities, bedrocktypes.ModelModalityImage)
		hasImageOut := hasModality(fm.OutputModalities, bedrocktypes.ModelModalityImage)
		foundationsForProfiles[id] = supportsTextOutput(fm) && !hasDisallowedOutputModalities(fm)
		chatCandidate := onDemand && foundationsForProfiles[id]

		if chatCandidate {
			chatLike++
			if !seen[id] {
				seen[id] = true
				discovered = append(discovered, id)
			}
		}
		if hasImageIn {
			imageIn++
		}
		if hasImageOut {
			imageOut++
		}

		lifecycle := ""
		if fm.ModelLifecycle != nil {
			lifecycle = string(fm.ModelLifecycle.Status)
		}
		streaming := ""
		if fm.ResponseStreamingSupported != nil {
			streaming = boolStr(*fm.ResponseStreamingSupported)
		}

		_ = fw.Write([]string{
			id,
			provider,
			deref(fm.ModelName),
			lifecycle,
			joinInferenceTypes(fm.InferenceTypesSupported),
			joinModalities(fm.InputModalities),
			joinModalities(fm.OutputModalities),
			streaming,
			joinCustomizations(fm.CustomizationsSupported),
			boolStr(onDemand),
			boolStr(textOut),
			boolStr(hasImageIn),
			boolStr(hasImageOut),
			boolStr(chatCandidate),
		})
	}

	profilesFile, err := os.Create(profilesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", profilesPath, err)
		os.Exit(1)
	}
	defer profilesFile.Close()

	pw := csv.NewWriter(profilesFile)
	defer pw.Flush()
	_ = pw.Write([]string{"profile_id", "profile_name", "type", "status", "model_arns"})

	profileTotal := 0
	systemProfiles := 0
	appProfiles := 0
	var nextToken *string
	for {
		out, err := client.ListInferenceProfiles(ctx, &bedrock.ListInferenceProfilesInput{NextToken: nextToken})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ListInferenceProfiles: %v\n", err)
			os.Exit(1)
		}

		for _, p := range out.InferenceProfileSummaries {
			profileTotal++

				if p.Type == bedrocktypes.InferenceProfileTypeSystemDefined {
					systemProfiles++
					if enableCross {
						id := strings.TrimSpace(deref(p.InferenceProfileId))
						if id != "" && !seen[id] && profileSupportsTextOnlyOutputs(p, foundationsForProfiles) {
							seen[id] = true
							discovered = append(discovered, id)
						}
					}
				}
				if p.Type == bedrocktypes.InferenceProfileTypeApplication {
					appProfiles++
					if enableApp {
						id := strings.TrimSpace(deref(p.InferenceProfileId))
						if id != "" && !seen[id] && profileSupportsTextOnlyOutputs(p, foundationsForProfiles) {
							seen[id] = true
							discovered = append(discovered, id)
						}
					}
				}

			modelArns := make([]string, 0, len(p.Models))
			for _, m := range p.Models {
				if m.ModelArn != nil {
					modelArns = append(modelArns, *m.ModelArn)
				}
			}
			sort.Strings(modelArns)

			_ = pw.Write([]string{
				deref(p.InferenceProfileId),
				deref(p.InferenceProfileName),
				string(p.Type),
				string(p.Status),
				strings.Join(modelArns, "|"),
			})
		}

		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}

	sort.Strings(discovered)
	if err := os.WriteFile(discoveredPath, []byte(strings.Join(discovered, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", discoveredPath, err)
		os.Exit(1)
	}

	providers := make([]string, 0, len(providerCounts))
	for p := range providerCounts {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	summary := []string{
		fmt.Sprintf("region=%s", region),
		fmt.Sprintf("enable_cross_region_inference=%v", enableCross),
		fmt.Sprintf("enable_app_inference_profiles=%v", enableApp),
		"",
		fmt.Sprintf("foundation_raw=%d", foundationRaw),
		fmt.Sprintf("foundation_chat_like_candidates(on_demand+text)=%d", chatLike),
		fmt.Sprintf("foundation_with_image_input=%d", imageIn),
		fmt.Sprintf("foundation_with_image_output=%d", imageOut),
		fmt.Sprintf("inference_profiles_total=%d", profileTotal),
		fmt.Sprintf("inference_profiles_system=%d", systemProfiles),
		fmt.Sprintf("inference_profiles_application=%d", appProfiles),
		fmt.Sprintf("startup_discovered_equivalent=%d", len(discovered)),
		"",
		"provider_counts_foundation_raw:",
	}
	for _, p := range providers {
		summary = append(summary, fmt.Sprintf("%s=%d", p, providerCounts[p]))
	}
	if err := os.WriteFile(summaryPath, []byte(strings.Join(summary, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", summaryPath, err)
		os.Exit(1)
	}

	fmt.Println("generated:")
	fmt.Printf("- %s\n", foundationPath)
	fmt.Printf("- %s\n", profilesPath)
	fmt.Printf("- %s\n", discoveredPath)
	fmt.Printf("- %s\n", summaryPath)
}
EOF

go run "${TMP_GO}" --out-dir "${OUT_DIR}"
