package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type RuntimeSettings struct {
	AppPort               string
	DatabasePath          string
	PrivateMembersPath    string
	FirstMonday           string
	SyncEnabled           bool
	SyncToken             string
	HotSlotBluePort       string
	HotSlotGreenPort      string
	HotSwitchDrainSeconds string
}

type envLine struct {
	Raw      string
	Key      string
	HasValue bool
}

func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		AppPort:               "3000",
		DatabasePath:          "../data/personnel.db",
		PrivateMembersPath:    "../data/member.json",
		FirstMonday:           "20260302",
		SyncEnabled:           false,
		SyncToken:             "",
		HotSlotBluePort:       "18081",
		HotSlotGreenPort:      "18082",
		HotSwitchDrainSeconds: "5",
	}
}

func LoadRuntimeSettings(envPath string) (RuntimeSettings, error) {
	lines, values, err := readEnvFile(envPath)
	if err != nil {
		return RuntimeSettings{}, err
	}
	_ = lines

	defaults := DefaultRuntimeSettings()
	settings := RuntimeSettings{
		AppPort:               valueOrDefault(values["APP_PORT"], defaults.AppPort),
		DatabasePath:          valueOrDefault(values["DATABASE_PATH"], defaults.DatabasePath),
		PrivateMembersPath:    valueOrDefault(values["PRIVATE_MEMBERS_PATH"], defaults.PrivateMembersPath),
		FirstMonday:           valueOrDefault(values["FIRST_MONDAY"], defaults.FirstMonday),
		SyncEnabled:           parseBool(values["SYNC_ENABLED"], defaults.SyncEnabled),
		SyncToken:             valueOrDefault(values["SYNC_TOKEN"], defaults.SyncToken),
		HotSlotBluePort:       valueOrDefault(values["HOT_SLOT_BLUE_PORT"], defaults.HotSlotBluePort),
		HotSlotGreenPort:      valueOrDefault(values["HOT_SLOT_GREEN_PORT"], defaults.HotSlotGreenPort),
		HotSwitchDrainSeconds: valueOrDefault(values["HOT_SWITCH_DRAIN_SECONDS"], defaults.HotSwitchDrainSeconds),
	}

	return settings, nil
}

func SaveRuntimeSettings(envPath string, settings RuntimeSettings) error {
	lines, values, err := readEnvFile(envPath)
	if err != nil {
		return err
	}

	values["APP_PORT"] = strings.TrimSpace(settings.AppPort)
	values["DATABASE_PATH"] = strings.TrimSpace(settings.DatabasePath)
	values["PRIVATE_MEMBERS_PATH"] = strings.TrimSpace(settings.PrivateMembersPath)
	values["FIRST_MONDAY"] = strings.TrimSpace(settings.FirstMonday)
	values["SYNC_ENABLED"] = strconv.FormatBool(settings.SyncEnabled)
	values["SYNC_TOKEN"] = strings.TrimSpace(settings.SyncToken)
	values["HOT_SLOT_BLUE_PORT"] = strings.TrimSpace(settings.HotSlotBluePort)
	values["HOT_SLOT_GREEN_PORT"] = strings.TrimSpace(settings.HotSlotGreenPort)
	values["HOT_SWITCH_DRAIN_SECONDS"] = strings.TrimSpace(settings.HotSwitchDrainSeconds)

	targetKeys := []string{
		"APP_PORT",
		"DATABASE_PATH",
		"PRIVATE_MEMBERS_PATH",
		"FIRST_MONDAY",
		"SYNC_ENABLED",
		"SYNC_TOKEN",
		"HOT_SLOT_BLUE_PORT",
		"HOT_SLOT_GREEN_PORT",
		"HOT_SWITCH_DRAIN_SECONDS",
	}

	seen := map[string]bool{}
	output := make([]string, 0, len(lines)+len(targetKeys))
	for _, line := range lines {
		if line.HasValue {
			if _, ok := values[line.Key]; ok {
				output = append(output, fmt.Sprintf("%s=%s", line.Key, values[line.Key]))
				seen[line.Key] = true
				continue
			}
		}
		output = append(output, line.Raw)
	}

	for _, key := range targetKeys {
		if seen[key] {
			continue
		}
		output = append(output, fmt.Sprintf("%s=%s", key, values[key]))
	}

	content := strings.Join(output, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return os.WriteFile(envPath, []byte(content), 0644)
}

func readEnvFile(envPath string) ([]envLine, map[string]string, error) {
	content, err := os.ReadFile(envPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read env file %s: %w", envPath, err)
	}

	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	lines := make([]envLine, 0, len(rawLines))
	values := map[string]string{}
	for _, rawLine := range rawLines {
		key, value, ok := parseEnvAssignment(rawLine)
		if ok {
			lines = append(lines, envLine{Raw: rawLine, Key: key, HasValue: true})
			values[key] = value
			continue
		}
		lines = append(lines, envLine{Raw: rawLine})
	}

	return lines, values, nil
}

func parseEnvAssignment(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	parts := strings.SplitN(trimmed, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}

	return key, strings.TrimSpace(parts[1]), true
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func parseBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
