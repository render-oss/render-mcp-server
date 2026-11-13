package cmd

import (
	"os"
	"slices"
	"strings"

	"github.com/render-oss/render-mcp-server/pkg/logging"
	"github.com/render-oss/render-mcp-server/pkg/workflow"
)

const featuresEnabledEnv = "FEATURES_ENABLED"

// Features listed here are off unless named in FEATURES_ENABLED.
const featureDurability21 = "durability-2.1"

var knownFeatures = []string{featureDurability21}

// enabledFeatures reads the comma-separated FEATURES_ENABLED list. An unknown
// name stops startup rather than silently leaving a feature off.
func enabledFeatures() map[string]bool {
	enabled := map[string]bool{}
	for _, name := range strings.Split(os.Getenv(featuresEnabledEnv), ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if !slices.Contains(knownFeatures, name) {
			logging.Fatal("unknown feature",
				logging.OneOffField("env", featuresEnabledEnv),
				logging.OneOffField("feature", name),
				logging.OneOffField("known_features", knownFeatures),
			)
		}
		enabled[name] = true
	}
	return enabled
}

func workflowOptions() workflow.Options {
	opts := workflow.Options{Durability21: enabledFeatures()[featureDurability21]}
	logging.Info("Workflows Durability 2.1", logging.OneOffField("enabled", opts.Durability21))
	return opts
}
