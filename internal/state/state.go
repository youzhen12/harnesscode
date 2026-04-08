package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Feature 表示一条功能项。
type Feature struct {
	ID           int    `json:"id"`
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	Module       string `json:"module,omitempty"`
	Status       string `json:"status"` // pending | completed | other
	Dependencies []int  `json:"dependencies,omitempty"`
}

// FeatureList 支持两种格式：
// 1) 直接是 []Feature
// 2) 包装为 {"features": [...]}
type FeatureList struct {
	Features []Feature `json:"features"`
}

// MissingItem/Info 对应 missing_info.json
type MissingItem struct {
	ID          int       `json:"id"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type MissingInfo struct {
	Items []MissingItem `json:"missing_items"`
}

// LoadFeatureList 从 .harnesscode/feature_list.json 读取特性列表。
func LoadFeatureList(projectRoot string) (*FeatureList, error) {
	path := filepath.Join(projectRoot, ".harnesscode", "feature_list.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 尝试两种结构。
	var direct []Feature
	if err := json.Unmarshal(data, &direct); err == nil && len(direct) > 0 {
		return &FeatureList{Features: normalizeFeatures(direct)}, nil
	}

	var wrapped FeatureList
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	wrapped.Features = normalizeFeatures(wrapped.Features)
	return &wrapped, nil
}

// SaveFeatureList 覆盖写入 feature_list.json（使用 wrapped 结构）。
func SaveFeatureList(projectRoot string, fl *FeatureList) error {
	path := filepath.Join(projectRoot, ".harnesscode", "feature_list.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fl.Features = normalizeFeatures(fl.Features)
	out, err := json.MarshalIndent(fl, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func normalizeFeatures(features []Feature) []Feature {
	for i := range features {
		features[i].Status = normalizeStatus(features[i].Status)
	}
	return features
}

func normalizeStatus(s string) string {
	if s == "" {
		return "pending"
	}
	sLower := s
	if sLower == "Completed" || sLower == "Done" || sLower == "Finish" || sLower == "Finished" || sLower == "Complete" || sLower == "Passed" {
		return "completed"
	}
	return s
}

// LoadMissingInfo 从 .harnesscode/missing_info.json 读取。
func LoadMissingInfo(projectRoot string) (*MissingInfo, error) {
	path := filepath.Join(projectRoot, ".harnesscode", "missing_info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mi MissingInfo
	if err := json.Unmarshal(data, &mi); err != nil {
		return nil, err
	}
	return &mi, nil
}

// SaveMissingInfo 写回 missing_info.json。
func SaveMissingInfo(projectRoot string, mi *MissingInfo) error {
	path := filepath.Join(projectRoot, ".harnesscode", "missing_info.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(mi, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
