// SPDX-License-Identifier: GPL-3.0-or-later

// Package webhook sends bottleneck detection results to external services.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Config holds webhook notification configuration.
type Config struct {
	URL               string  `yaml:"url"`                // Webhook endpoint URL
	MinConfidence     float64 `yaml:"min_confidence"`     // Only notify if confidence >= this (default: 0.7)
	CooldownMinutes   int     `yaml:"cooldown_minutes"`   // Minimum time between notifications (default: 15)
	IncludeEvidence   bool    `yaml:"include_evidence"`   // Include detailed evidence in payload
}

// Payload is the JSON body sent to the webhook endpoint.
type Payload struct {
	Timestamp      time.Time        `json:"timestamp"`
	NodeID         string           `json:"node_id"`
	BottleneckType string           `json:"bottleneck_type"`
	Confidence     float64          `json:"confidence"`
	Explanation    string           `json:"explanation"`
	Evidence       []EvidenceDetail `json:"evidence,omitempty"`
}

// EvidenceDetail is a supporting metric in the webhook payload.
type EvidenceDetail struct {
	Context   string  `json:"context"`
	Dimension string  `json:"dimension"`
	AvgValue  float64 `json:"avg_value,omitempty"`
	MaxValue  float64 `json:"max_value,omitempty"`
}

// Notifier sends webhook notifications when bottlenecks are detected.
type Notifier struct {
	configs  []Config
	client   *http.Client
	logger   *slog.Logger
	lastSent map[string]time.Time // key: "node_id:bottleneck_type" -> last sent time
}

// NewNotifier creates a webhook notifier with the given configurations.
func NewNotifier(configs []Config, logger *slog.Logger) *Notifier {
	return &Notifier{
		configs:  configs,
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
		lastSent: make(map[string]time.Time),
	}
}

// Notify sends a bottleneck detection to all configured webhooks if it meets
// the minimum confidence threshold and cooldown period.
func (n *Notifier) Notify(ctx context.Context, payload Payload) {
	if len(n.configs) == 0 || payload.BottleneckType == "none" {
		return
	}

	cooldownKey := fmt.Sprintf("%s:%s", payload.NodeID, payload.BottleneckType)

	for _, cfg := range n.configs {
		if cfg.URL == "" {
			continue
		}

		minConf := cfg.MinConfidence
		if minConf == 0 {
			minConf = 0.7
		}
		if payload.Confidence < minConf {
			continue
		}

		cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
		if cooldown == 0 {
			cooldown = 15 * time.Minute
		}
		if last, ok := n.lastSent[cooldownKey]; ok && time.Since(last) < cooldown {
			n.logger.Debug("webhook cooldown active", "key", cooldownKey,
				"remaining", cooldown-time.Since(last))
			continue
		}

		p := payload
		p.Timestamp = time.Now().UTC()
		if !cfg.IncludeEvidence {
			p.Evidence = nil
		}

		if err := n.send(ctx, cfg.URL, p); err != nil {
			n.logger.Error("webhook notification failed", "url", cfg.URL, "error", err)
			continue
		}

		n.lastSent[cooldownKey] = time.Now()
		n.logger.Info("webhook notification sent",
			"url", cfg.URL,
			"node_id", payload.NodeID,
			"type", payload.BottleneckType,
			"confidence", payload.Confidence,
		)
	}
}

func (n *Notifier) send(ctx context.Context, url string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "netdata-postgres-mcp/webhook")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	return nil
}
