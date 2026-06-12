package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enough/enough/backend/enoughhome"
)

type SkillReadinessStatus string

const (
	ReadinessAvailable   SkillReadinessStatus = "available"
	ReadinessSetupNeeded SkillReadinessStatus = "setup_needed"
	ReadinessUnsupported SkillReadinessStatus = "unsupported"
)

type RequiredEnvVar struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	Help        string `json:"help,omitempty"`
	RequiredFor string `json:"required_for,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

type CollectSecretEntry struct {
	EnvVar      string `json:"env_var"`
	Prompt      string `json:"prompt"`
	ProviderURL string `json:"provider_url,omitempty"`
	Secret      bool   `json:"secret"`
}

type SetupBlock struct {
	Help           *string               `json:"help"`
	CollectSecrets []CollectSecretEntry `json:"collect_secrets"`
}

var envVarNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func LoadEnoughEnv() map[string]string {
	envMap := make(map[string]string)
	home := enoughhome.HomeDir()
	envPath := filepath.Join(home, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return envMap
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			envMap[k] = v
		}
	}
	return envMap
}

func isEnvVarSet(name string, envMap map[string]string) bool {
	if os.Getenv(name) != "" {
		return true
	}
	if v, ok := envMap[name]; ok && v != "" {
		return true
	}
	return false
}

func normalizeSetupMetadata(fm map[string]interface{}) SetupBlock {
	setupVal, ok := fm["setup"]
	if !ok || setupVal == nil {
		return SetupBlock{CollectSecrets: []CollectSecretEntry{}}
	}
	setupMap, ok := setupVal.(map[string]interface{})
	if !ok {
		return SetupBlock{CollectSecrets: []CollectSecretEntry{}}
	}

	var helpText *string
	if h, ok := setupMap["help"].(string); ok && strings.TrimSpace(h) != "" {
		trimmed := strings.TrimSpace(h)
		helpText = &trimmed
	}

	var collectSecrets []CollectSecretEntry
	csVal := setupMap["collect_secrets"]
	if csVal != nil {
		var rawList []interface{}
		if singleMap, ok := csVal.(map[string]interface{}); ok {
			rawList = []interface{}{singleMap}
		} else if list, ok := csVal.([]interface{}); ok {
			rawList = list
		}
		for _, item := range rawList {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			var envVar string
			if ev, ok := itemMap["env_var"].(string); ok {
				envVar = strings.TrimSpace(ev)
			}
			if envVar == "" {
				continue
			}
			prompt := fmt.Sprintf("Enter value for %s", envVar)
			if p, ok := itemMap["prompt"].(string); ok && strings.TrimSpace(p) != "" {
				prompt = strings.TrimSpace(p)
			}
			var providerURL string
			if pu, ok := itemMap["provider_url"].(string); ok {
				providerURL = strings.TrimSpace(pu)
			} else if u, ok := itemMap["url"].(string); ok {
				providerURL = strings.TrimSpace(u)
			}

			secret := true
			if sec, ok := itemMap["secret"]; ok {
				if secBool, ok := sec.(bool); ok {
					secret = secBool
				}
			}
			collectSecrets = append(collectSecrets, CollectSecretEntry{
				EnvVar:      envVar,
				Prompt:      prompt,
				ProviderURL: providerURL,
				Secret:      secret,
			})
		}
	}

	return SetupBlock{
		Help:           helpText,
		CollectSecrets: collectSecrets,
	}
}

func getRequiredEnvironmentVariables(fm map[string]interface{}) []RequiredEnvVar {
	setup := normalizeSetupMetadata(fm)
	var required []RequiredEnvVar
	seen := make(map[string]bool)

	appendRequired := func(entry map[string]interface{}) {
		var name string
		if n, ok := entry["name"].(string); ok {
			name = strings.TrimSpace(n)
		} else if ev, ok := entry["env_var"].(string); ok {
			name = strings.TrimSpace(ev)
		}
		if name == "" || seen[name] {
			return
		}
		if !envVarNameRe.MatchString(name) {
			return
		}

		prompt := fmt.Sprintf("Enter value for %s", name)
		if p, ok := entry["prompt"].(string); ok && strings.TrimSpace(p) != "" {
			prompt = strings.TrimSpace(p)
		}

		var helpText string
		if h, ok := entry["help"].(string); ok && strings.TrimSpace(h) != "" {
			helpText = strings.TrimSpace(h)
		} else if pu, ok := entry["provider_url"].(string); ok && strings.TrimSpace(pu) != "" {
			helpText = strings.TrimSpace(pu)
		} else if u, ok := entry["url"].(string); ok && strings.TrimSpace(u) != "" {
			helpText = strings.TrimSpace(u)
		} else if setup.Help != nil {
			helpText = *setup.Help
		}

		var requiredFor string
		if rf, ok := entry["required_for"].(string); ok {
			requiredFor = strings.TrimSpace(rf)
		}

		var optional bool
		if opt, ok := entry["optional"].(bool); ok {
			optional = opt
		}

		seen[name] = true
		required = append(required, RequiredEnvVar{
			Name:        name,
			Prompt:      prompt,
			Help:        helpText,
			RequiredFor: requiredFor,
			Optional:    optional,
		})
	}

	// 1. required_environment_variables
	reqRaw := fm["required_environment_variables"]
	if reqRaw != nil {
		var rawList []interface{}
		if singleStr, ok := reqRaw.(string); ok {
			rawList = []interface{}{singleStr}
		} else if singleMap, ok := reqRaw.(map[string]interface{}); ok {
			rawList = []interface{}{singleMap}
		} else if list, ok := reqRaw.([]interface{}); ok {
			rawList = list
		}
		for _, item := range rawList {
			if s, ok := item.(string); ok {
				appendRequired(map[string]interface{}{"name": s})
			} else if m, ok := item.(map[string]interface{}); ok {
				appendRequired(m)
			}
		}
	}

	// 2. setup.collect_secrets
	for _, item := range setup.CollectSecrets {
		appendRequired(map[string]interface{}{
			"name":         item.EnvVar,
			"prompt":       item.Prompt,
			"provider_url": item.ProviderURL,
		})
	}

	// 3. legacy prerequisites.env_vars
	if prereqs, ok := fm["prerequisites"].(map[string]interface{}); ok {
		if envVarsVal := prereqs["env_vars"]; envVarsVal != nil {
			var list []string
			if s, ok := envVarsVal.(string); ok && strings.TrimSpace(s) != "" {
				list = []string{strings.TrimSpace(s)}
			} else if l, ok := envVarsVal.([]interface{}); ok {
				for _, it := range l {
					if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
						list = append(list, strings.TrimSpace(s))
					}
				}
			}
			for _, ev := range list {
				appendRequired(map[string]interface{}{"name": ev})
			}
		}
	}

	return required
}

func CheckSkillsRequirements() bool {
	return true
}
