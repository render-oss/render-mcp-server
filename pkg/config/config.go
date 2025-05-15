package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/render-oss/cli/pkg/cfg"
)

const currentVersion = 1
const defaultDashboardURL = "https://dashboard.render.com"

var defaultConfigPath string

const configPathEnvKey = "RENDER_CLI_CONFIG_PATH"
const workspaceEnvKey = "RENDER_WORKSPACE"

var ErrNoWorkspace = errors.New("no workspace set. Use `render workspace set` to set a workspace")
var ErrLogin = errors.New("run `render login` to authenticate")

type Config struct {
	Version       int    `yaml:"version"`
	Workspace     string `yaml:"workspace"`
	WorkspaceName string `yaml:"workspace_name"`
	ProjectFilter string `yaml:"project_filter,omitempty"` // Project ID for filtering
	ProjectName   string `yaml:"project_name,omitempty"`   // Project name for display

	APIConfig    `yaml:"api"`
	DashboardURL string `yaml:"dashboard_url,omitempty"`
}

type APIConfig struct {
	Key          string `yaml:"key,omitempty"`
	ExpiresAt    int64  `yaml:"expires_at,omitempty"`
	Host         string `json:"host,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	defaultConfigPath = filepath.Join(home, ".render", "cli.yaml")
}

func DefaultAPIConfig() (APIConfig, error) {
	apiCfg := APIConfig{
		Key:  cfg.GetAPIKey(),
		Host: cfg.GetHost(),
	}

	var err error
	if apiCfg.Key == "" {
		apiCfg, err = getAPIConfig()
		if err != nil || apiCfg.Key == "" {
			return APIConfig{}, ErrLogin
		}
	}

	if apiCfg.Host == "" {
		apiCfg.Host = cfg.GetHost()
	}

	return apiCfg, nil
}

func DashboardURL() string {
	cfg, err := Load()
	if err != nil {
		return defaultDashboardURL
	}
	return cfg.DashboardURL
}

func SetDashboardURL(u string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	fullURL, err := url.Parse(u)
	if err != nil {
		return err
	}

	fullURL.Path = ""

	cfg.DashboardURL = fullURL.String()
	return cfg.Persist()
}

func getConfigPath() string {
	if path := os.Getenv(configPathEnvKey); path != "" {
		return path
	}
	return defaultConfigPath
}

func expandPath(path string) (string, error) {
	if path == "~" || len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return path, nil
}

func WorkspaceID() (string, error) {
	if workspaceID := os.Getenv(workspaceEnvKey); workspaceID != "" {
		return workspaceID, nil
	}

	cfg, err := Load()
	if err != nil {
		return "", err
	}
	if cfg.Workspace == "" {
		return "", ErrNoWorkspace
	}
	return cfg.Workspace, nil
}

func IsWorkspaceSet() bool {
	id, _ := WorkspaceID()
	return id != ""
}

func WorkspaceName() (string, error) {
	if workspaceID := os.Getenv(workspaceEnvKey); workspaceID != "" {
		return workspaceID, nil
	}

	cfg, err := Load()
	if err != nil {
		return "", err
	}
	if cfg.WorkspaceName == "" {
		return "", ErrNoWorkspace
	}
	return cfg.WorkspaceName, nil
}

func GetProjectFilter() (projectID string, projectName string, err error) {
	cfg, err := Load()
	if err != nil {
		return "", "", err
	}
	return cfg.ProjectFilter, cfg.ProjectName, nil
}

func SetProjectFilter(projectID string, projectName string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.ProjectFilter = projectID
	cfg.ProjectName = projectName
	return cfg.Persist()
}

func ClearProjectFilter() error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.ProjectFilter = ""
	cfg.ProjectName = ""
	return cfg.Persist()
}

func getAPIConfig() (APIConfig, error) {
	cfg, err := Load()
	if err != nil {
		return APIConfig{}, err
	}

	return cfg.APIConfig, nil
}

func SetAPIConfig(input APIConfig) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	cfg.Host = input.Host
	cfg.Key = input.Key
	cfg.ExpiresAt = input.ExpiresAt
	cfg.RefreshToken = input.RefreshToken
	return cfg.Persist()
}

func Load() (*Config, error) {
	path, err := expandPath(getConfigPath())
	if err != nil {
		return nil, err
	}

	// Ignore the error if we can't chmod try to continue
	_ = os.Chmod(filepath.Dir(path), 0755)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Version: currentVersion}, nil
		}
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) Persist() error {
	path, err := expandPath(getConfigPath())
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	// Ignore the error if we can't chmod try to continue
	_ = os.Chmod(filepath.Dir(path), 0755)

	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
