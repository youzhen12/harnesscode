package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type record struct {
	Time     time.Time `json:"time"`
	Success  bool      `json:"success"`
	Duration float64   `json:"duration"`
}

type agentMetrics struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Recent  []record `json:"recent"`
}

type payload struct {
	Agents map[string]*agentMetrics `json:"agents"`
}

// Store 负责单个项目的指标存储。
type Store struct {
	filePath string
}

// NewStore 创建或打开指定路径的 metrics 存储。
func NewStore(projectRoot string, projectID string) (*Store, error) {
	learningDir := filepath.Join(os.Getenv("HOME"), ".harnesscode", "projects", projectID, "learning")
	if err := os.MkdirAll(learningDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(learningDir, "metrics.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		empty := payload{Agents: map[string]*agentMetrics{}}
		b, _ := json.Marshal(empty)
		_ = os.WriteFile(path, b, 0o644)
	}
	return &Store{filePath: path}, nil
}

// RecordSession 记录一次 agent 运行。
func (s *Store) RecordSession(agent string, success bool, durationSeconds float64) error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var p payload
	if len(data) == 0 {
		p.Agents = map[string]*agentMetrics{}
	} else if err := json.Unmarshal(data, &p); err != nil {
		p.Agents = map[string]*agentMetrics{}
	}

	am, ok := p.Agents[agent]
	if !ok {
		am = &agentMetrics{Recent: make([]record, 0, 50)}
		p.Agents[agent] = am
	}

	am.Total++
	if success {
		am.Success++
	}
	am.Recent = append(am.Recent, record{
		Time:     time.Now().UTC(),
		Success:  success,
		Duration: durationSeconds,
	})
	if len(am.Recent) > 50 {
		am.Recent = am.Recent[len(am.Recent)-50:]
	}

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, out, 0o644)
}

// SuccessRate 返回最近 N 次中的成功率。
func (s *Store) SuccessRate(agent string, recentN int) (float64, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return 0, err
	}
	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return 0, err
	}
	am, ok := p.Agents[agent]
	if !ok || len(am.Recent) == 0 {
		return 0, nil
	}
	if recentN <= 0 || recentN > len(am.Recent) {
		recentN = len(am.Recent)
	}
	recent := am.Recent[len(am.Recent)-recentN:]
	var okCount int
	for _, r := range recent {
		if r.Success {
			okCount++
		}
	}
	return float64(okCount) / float64(len(recent)), nil
}
