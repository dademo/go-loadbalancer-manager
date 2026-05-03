package haproxycfg

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Managed HAProxy configuration block markers.
const (
	ManagedBlockStart = "# BEGIN LBM MANAGED CONFIGURATIONS"
	ManagedBlockEnd   = "# END LBM MANAGED CONFIGURATIONS"
	ManagedConfigLine = "# LBM_CONFIG "
)

// ManagedBlockBounds returns the start index, end index, and split lines of the managed block
// within content. Returns (-1, -1, lines) if no complete managed block is found.
func ManagedBlockBounds(content string) (int, int, []string) {
	lines := strings.Split(content, "\n")
	start := -1
	end := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case ManagedBlockStart:
			if start == -1 {
				start = i
			}
		case ManagedBlockEnd:
			if start != -1 && end == -1 {
				end = i
			}
		}
	}

	return start, end, lines
}

// MergeManagedBlock replaces (or appends) the managed block inside base with a freshly
// rendered block for the provided configurations.
func MergeManagedBlock(base string, configurations []HaproxyConfiguration) (string, error) {
	block, err := RenderManagedBlock(configurations)
	if err != nil {
		return "", err
	}

	start, end, lines := ManagedBlockBounds(base)
	if start != -1 && end != -1 && start < end {
		replaced := append([]string{}, lines[:start]...)
		replaced = append(replaced, strings.Split(block, "\n")...)
		replaced = append(replaced, lines[end+1:]...)
		return strings.Join(replaced, "\n"), nil
	}

	trimmed := strings.TrimRight(base, "\n")
	if trimmed == "" {
		return block + "\n", nil
	}

	return trimmed + "\n\n" + block + "\n", nil
}

// RenderManagedBlock renders the full managed block: the serialised configuration comments
// followed by the frontend and backend sections for every configuration.
func RenderManagedBlock(configurations []HaproxyConfiguration) (string, error) {
	sorted := make([]HaproxyConfiguration, 0, len(configurations))
	for _, configuration := range configurations {
		cloned := CloneConfiguration(configuration)
		cloned.Name = NormalizeConfigurationName(cloned.Name)
		sorted = append(sorted, cloned)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	lines := []string{ManagedBlockStart}
	for _, configuration := range sorted {
		payload, err := json.Marshal(configuration)
		if err != nil {
			return "", fmt.Errorf("unable to serialize managed configuration %q: %w", configuration.Name, err)
		}
		lines = append(lines, ManagedConfigLine+string(payload))
	}

	for _, configuration := range sorted {
		frontendLines, err := RenderFrontendSection(configuration)
		if err != nil {
			return "", err
		}
		lines = append(lines, frontendLines...)
		lines = append(lines, RenderBackendSection(configuration)...)
	}

	lines = append(lines, ManagedBlockEnd)
	return strings.Join(lines, "\n"), nil
}
