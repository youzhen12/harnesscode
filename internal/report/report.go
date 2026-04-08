package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"harnesscode-go/internal/state"
)

// SendWebhook 向 webhookURL 发送简单文本消息。
func SendWebhook(webhookURL, text string) error {
	if webhookURL == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %s", resp.Status)
	}
	return nil
}

// GenerateDevReport 生成简要开发报告 markdown，返回路径。
func GenerateDevReport(projectRoot, projectID string, reportType string) (string, error) {
	if reportType == "" {
		reportType = "final"
	}

	// 统计 feature 状态。
	var total, completed, pending int
	if fl, err := state.LoadFeatureList(projectRoot); err == nil {
		for _, f := range fl.Features {
			if f.Status == "completed" {
				completed++
			} else if f.Status == "pending" {
				pending++
			}
		}
		total = len(fl.Features)
	}

	reportsDir := filepath.Join(projectRoot, ".harnesscode", "reports")
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102_150405")
	file := filepath.Join(reportsDir, fmt.Sprintf("dev-report-%s-%s.md", reportType, stamp))

	content := fmt.Sprintf(`# Development Report (%s)

> Generated: %s
> Project: %s

---

## Summary

| Metric | Value |
|--------|-------|
| Total Features | %d |
| Completed | %d |
| Pending | %d |

`, reportType, time.Now().Format("2006-01-02 15:04:05"), projectID, total, completed, pending)

	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return "", err
	}
	return file, nil
}
