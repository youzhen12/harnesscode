package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manager 负责写入 bug pattern 知识文档。
type Manager struct {
	projectID string
}

func NewManager(projectID string) *Manager {
	return &Manager{projectID: projectID}
}

// SaveBugPattern 将缺陷模式写入 markdown 文件，并返回路径。
func (m *Manager) SaveBugPattern(summary, location, action string) (string, error) {
	base := filepath.Join(os.Getenv("HOME"), ".harnesscode", "projects", m.projectID, "learning", "docs", "solutions", "bugs")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("bug-%s.md", time.Now().UTC().Format("20060102_150405"))
	path := filepath.Join(base, filename)

	content := fmt.Sprintf(`# %s

> Auto-generated: %s
> Project: %s

## Location

```
%s
```

## Solution

%s

## Tags

- auto-learned
- bug-pattern
`,
		summary,
		time.Now().UTC().Format("2006-01-02 15:04:05"),
		m.projectID,
		location,
		action,
	)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
