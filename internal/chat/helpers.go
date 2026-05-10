package chat

import (
	"encoding/json"
	"os"

	"github.com/brainplusplus/9ed/internal/chat/acp"
	"github.com/brainplusplus/9ed/internal/chat/acpinstall"
)

func jsonUnmarshal(data json.RawMessage, v any) error {
	return json.Unmarshal(data, v)
}

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeFileContent(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func currentWorkingDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func InstallACPAdapter(agentID string) (string, error) {
	return acpinstall.EnsureInstalled(agentID)
}

func convertConfigOptions(opts []acp.SessionConfigOption) []ConfigOptionInfo {
	result := make([]ConfigOptionInfo, len(opts))
	for i, opt := range opts {
		values := make([]ConfigValueInfo, len(opt.Options))
		for j, v := range opt.Options {
			values[j] = ConfigValueInfo{
				Value:       v.Value,
				Name:        v.Name,
				Description: v.Description,
			}
		}
		result[i] = ConfigOptionInfo{
			ID:           opt.ID,
			Name:         opt.Name,
			Description:  opt.Description,
			Category:     opt.Category,
			Type:         opt.Type,
			CurrentValue: opt.CurrentValue,
			Options:      values,
		}
	}
	return result
}
