// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// AlertRule defines a threshold-based alert evaluated against stored metrics.
type AlertRule struct {
	Name       string  `json:"name"`
	Context    string  `json:"context"`
	Dimension  string  `json:"dimension"`
	Operator   string  `json:"operator"` // ">", "<", ">=", "<="
	Threshold  float64 `json:"threshold"`
	Window     string  `json:"window"` // e.g. "-5m"
	Aggregator string  `json:"aggregator"` // "avg", "max", "min"
	Severity   string  `json:"severity"`   // "critical", "warning", "info"
}

// DefaultAlertRules are built-in alert rules evaluated by check_alerts.
var DefaultAlertRules = []AlertRule{
	{Name: "high_cpu_usage", Context: "system.cpu", Dimension: "user", Operator: ">", Threshold: 90, Window: "-5m", Aggregator: "avg", Severity: "critical"},
	{Name: "high_cpu_sustained", Context: "system.cpu", Dimension: "user", Operator: ">", Threshold: 70, Window: "-15m", Aggregator: "avg", Severity: "warning"},
	{Name: "high_iowait", Context: "system.cpu", Dimension: "iowait", Operator: ">", Threshold: 20, Window: "-5m", Aggregator: "avg", Severity: "warning"},
	{Name: "high_ram_usage", Context: "system.ram", Dimension: "used", Operator: ">", Threshold: 90, Window: "-5m", Aggregator: "avg", Severity: "warning"},
	{Name: "disk_util_critical", Context: "disk.util", Dimension: "", Operator: ">", Threshold: 95, Window: "-5m", Aggregator: "max", Severity: "critical"},
	{Name: "disk_util_warning", Context: "disk.util", Dimension: "", Operator: ">", Threshold: 80, Window: "-10m", Aggregator: "avg", Severity: "warning"},
	{Name: "swap_in_use", Context: "system.swap", Dimension: "used", Operator: ">", Threshold: 500, Window: "-5m", Aggregator: "avg", Severity: "warning"},
}

// AlertResult is the outcome of evaluating an alert rule.
type AlertResult struct {
	Name      string  `json:"name"`
	Status    string  `json:"status"` // "firing", "ok", "no_data"
	Severity  string  `json:"severity"`
	Value     float64 `json:"value,omitempty"`
	Threshold float64 `json:"threshold"`
	Operator  string  `json:"operator"`
	Window    string  `json:"window"`
	Message   string  `json:"message"`
}

// registerAlertTools adds the list_alerts and check_alerts MCP tools.
func (s *Server) registerAlertTools() {
	s.srv.AddTool(gomcp.NewTool(
		"list_alerts",
		gomcp.WithDescription("List all available alert rules that can be evaluated against stored metrics. Returns rule names, thresholds, and severity levels."),
	), s.handleListAlerts)

	s.srv.AddTool(gomcp.NewTool(
		"check_alerts",
		gomcp.WithDescription("Evaluate alert rules against stored metrics for a node. Returns which alerts are firing, OK, or have no data."),
		gomcp.WithString("node_id", gomcp.Required(), gomcp.Description("Node ID to check alerts for.")),
		gomcp.WithString("severity", gomcp.Description("Filter by severity: critical, warning, info. If omitted, all severities are checked.")),
	), s.handleCheckAlerts)
}

func (s *Server) handleListAlerts(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
	return jsonResult(DefaultAlertRules)
}

func (s *Server) handleCheckAlerts(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	severityFilter, _ := req.Params.Arguments["severity"].(string)

	var results []AlertResult

	for _, rule := range DefaultAlertRules {
		if severityFilter != "" && rule.Severity != severityFilter {
			continue
		}

		result := s.evaluateAlert(ctx, nodeID, rule)
		results = append(results, result)
	}

	return jsonResult(results)
}

func (s *Server) evaluateAlert(ctx context.Context, nodeID string, rule AlertRule) AlertResult {
	result := AlertResult{
		Name:      rule.Name,
		Severity:  rule.Severity,
		Threshold: rule.Threshold,
		Operator:  rule.Operator,
		Window:    rule.Window,
	}

	afterTime, err := parseTimeArg(rule.Window)
	if err != nil {
		result.Status = "no_data"
		result.Message = fmt.Sprintf("invalid window %q: %v", rule.Window, err)
		return result
	}

	// Build query based on aggregator
	aggFunc := "AVG"
	switch rule.Aggregator {
	case "max":
		aggFunc = "MAX"
	case "min":
		aggFunc = "MIN"
	}

	query := fmt.Sprintf(`
		SELECT COALESCE(%s(value), -1)
		FROM hardware_metric_samples
		WHERE node_id = $1 AND collected_at >= $2 AND context LIKE $3`,
		aggFunc)

	args := []interface{}{nodeID, afterTime, rule.Context + "%"}

	if rule.Dimension != "" {
		query += " AND dimension = $4"
		args = append(args, rule.Dimension)
	}

	var value float64
	err = s.pool.QueryRow(ctx, query, args...).Scan(&value)
	if err != nil || value == -1 {
		result.Status = "no_data"
		result.Message = "no metric data found in the specified window"
		return result
	}

	result.Value = round2(value)

	// Evaluate threshold
	var firing bool
	switch rule.Operator {
	case ">":
		firing = value > rule.Threshold
	case ">=":
		firing = value >= rule.Threshold
	case "<":
		firing = value < rule.Threshold
	case "<=":
		firing = value <= rule.Threshold
	}

	if firing {
		result.Status = "firing"
		result.Message = fmt.Sprintf("%s %s(value)=%.2f %s %.2f over %s",
			rule.Name, strings.ToLower(aggFunc), value, rule.Operator, rule.Threshold, rule.Window)
	} else {
		result.Status = "ok"
		result.Message = fmt.Sprintf("%s %s(value)=%.2f within threshold (%.2f)",
			rule.Name, strings.ToLower(aggFunc), value, rule.Threshold)
	}

	return result
}
