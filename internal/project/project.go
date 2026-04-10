package project

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 描述项目级配置，初版只保留最核心字段。
// 后续可以按需扩展。
type Config struct {
	ProjectID  string `yaml:"project_id"`
	Backend    string `yaml:"backend"`
	AutoCommit int    `yaml:"auto_commit"`
	WebhookURL string `yaml:"webhook_url,omitempty"`
	// ManualFeatures 为 true 时，表示 feature_list.json 完全由用户手动维护，
	// 初始化和循环过程中不应通过 initializer 自动从文档提取/重建特性列表。
	ManualFeatures bool `yaml:"manual_features,omitempty"`
}

// Paths 封装项目相关路径。
type Paths struct {
	Root          string
	HarnessDir    string
	ConfigPath    string
	ProjectIDPath string
}

// DetectPaths 基于工作目录推导项目路径结构。
func DetectPaths(root string) (*Paths, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	hDir := filepath.Join(root, ".harnesscode")
	return &Paths{
		Root:          root,
		HarnessDir:    hDir,
		ConfigPath:    filepath.Join(hDir, "config.yaml"),
		ProjectIDPath: filepath.Join(hDir, "project_id"),
	}, nil
}

// EnsureHarnessDir 确保 .harnesscode 目录存在。
func EnsureHarnessDir(p *Paths) error {
	return os.MkdirAll(p.HarnessDir, 0o755)
}

// GenerateProjectID 基于项目绝对路径生成稳定 ID。
func GenerateProjectID(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := md5.Sum([]byte(abs))
	return fmt.Sprintf("project-%s", hex.EncodeToString(sum[:])[:8])
}

// GetOrCreateProjectID 读取或创建项目 ID。
func GetOrCreateProjectID(p *Paths) (string, error) {
	if data, err := os.ReadFile(p.ProjectIDPath); err == nil {
		return string(bytesTrimSpace(data)), nil
	}

	if err := EnsureHarnessDir(p); err != nil {
		return "", err
	}
	id := GenerateProjectID(p.Root)
	if err := os.WriteFile(p.ProjectIDPath, []byte(id+"\n"), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

// LoadConfig 读取配置，如果不存在则返回空配置和 os.ErrNotExist。
func LoadConfig(p *Paths) (*Config, error) {
	data, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig 写入配置，必要时创建目录。
func SaveConfig(p *Paths, cfg *Config) error {
	if err := EnsureHarnessDir(p); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(p.ConfigPath, data, 0o644)
}

// bytesTrimSpace 是一个小工具，避免引入 bytes 包到外部调用处。
func bytesTrimSpace(b []byte) []byte {
	i := 0
	for ; i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t'); i++ {
	}
	j := len(b) - 1
	for ; j >= i && (b[j] == ' ' || b[j] == '\n' || b[j] == '\r' || b[j] == '\t'); j-- {
	}
	return b[i : j+1]
}
