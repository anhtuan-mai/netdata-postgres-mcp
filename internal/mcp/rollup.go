// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerRollupTools() {
	s.srv.AddTool(mcp.NewTool(
		"query_rollup_metrics",
		mcp.WithDescription("Query pre-aggregated rollup metrics. Automatically selects raw samples (<6h), hourly rollups (6h-7d), or daily rollups (>7d) based on time range."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID to query.")),
		mcp.WithString("context", mcp.Description("Filter by metric context (e.g. 'system.cpu').")),
		mcp.WithString("dimension", mcp.Description("Filter by dimension.")),
		mcp.WithString("instance", mcp.Description("Filter by instance.")),
		mcp.WithString("after", mcp.Required(), mcp.Description("Start time as ISO timestamp or relative duration like '-24h', '-7d'.")),
		mcp.WithString("before", mcp.Description("End time. Default is now.")),
		mcp.WithNumber("limit", mcp.Description("Maximum results. Default 500, max 5000.")),
	), s.handleQueryRollup)
}

type rollupRow struct {
	NodeID      string    `json:"node_id"`
	Bucket      time.Time `json:"bucket"`
	Context     string    `json:"context"`
	Dimension   string    `json:"dimension"`
	Instance    string    `json:"instance,omitempty"`
	Unit        string    `json:"unit,omitempty"`
	AvgValue    float64   `json:"avg_value"`
	MinValue    float64   `json:"min_value"`
	MaxValue    float64   `json:"max_value"`
	SampleCount int       `json:"sample_count"`
}

type rollupResult struct {
	NodeID     string      `json:"node_id"`
	Source     string      `json:"source"`
	From       time.Time   `json:"from"`
	To         time.Time   `json:"to"`
	RowCount   int         `json:"row_count"`
	Rows       []rollupRow `json:"rows"`
}

func (s *Server) handleQueryRollup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	ctxFilter, _ := req.Params.Arguments["context"].(string)
	dimension, _ := req.Params.Arguments["dimension"].(string)
	instance, _ := req.Params.Arguments["instance"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)
	beforeStr, _ := req.Params.Arguments["before"].(string)
	limitF, _ := req.Params.Arguments["limit"].(float64)

	limit := 500
	if limitF > 0 {
		limit = int(limitF)
	}
	if limit > 5000 {
		limit = 5000
	}

	afterTime, err := parseTimeArg(afterStr)
	if err != nil {
		return textResult(fmt.Sprintf("Invalid 'after': %v", err)), nil
	}

	beforeTime := time.Now().UTC()
	if beforeStr != "" {
		bt, err := parseTimeArg(beforeStr)
		if err != nil {
			return textResult(fmt.Sprintf("Invalid 'before': %v", err)), nil
		}
		beforeTime = bt
	}

	rangeWidth := beforeTime.Sub(afterTime)
	table, source := selectRollupTable(rangeWidth)

	query := fmt.Sprintf(`SELECT node_id, bucket, context, dimension, instance, unit,
		avg_value, min_value, max_value, sample_count
		FROM %s WHERE node_id = $1 AND bucket >= $2 AND bucket <= $3`, table)

	args := []interface{}{nodeID, afterTime, beforeTime}
	argIdx := 4

	if ctxFilter != "" {
		query += fmt.Sprintf(" AND context = $%d", argIdx)
		args = append(args, ctxFilter)
		argIdx++
	}
	if dimension != "" {
		query += fmt.Sprintf(" AND dimension = $%d", argIdx)
		args = append(args, dimension)
		argIdx++
	}
	if instance != "" {
		query += fmt.Sprintf(" AND instance = $%d", argIdx)
		args = append(args, instance)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY bucket DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error querying rollup: %v", err)), nil
	}
	defer rows.Close()

	var results []rollupRow
	for rows.Next() {
		var r rollupRow
		if err := rows.Scan(&r.NodeID, &r.Bucket, &r.Context, &r.Dimension,
			&r.Instance, &r.Unit, &r.AvgValue, &r.MinValue, &r.MaxValue, &r.SampleCount); err != nil {
			return textResult(fmt.Sprintf("Error scanning: %v", err)), nil
		}
		r.AvgValue = round2(r.AvgValue)
		r.MinValue = round2(r.MinValue)
		r.MaxValue = round2(r.MaxValue)
		results = append(results, r)
	}

	return jsonResult(rollupResult{
		NodeID:   nodeID,
		Source:   source,
		From:     afterTime,
		To:       beforeTime,
		RowCount: len(results),
		Rows:     results,
	})
}

func selectRollupTable(rangeWidth time.Duration) (table, source string) {
	switch {
	case rangeWidth > 7*24*time.Hour:
		return "hardware_metric_rollups_1d", "daily"
	case rangeWidth > 6*time.Hour:
		return "hardware_metric_rollups_1h", "hourly"
	default:
		return "hardware_metric_rollups_1h", "hourly"
	}
}

// selectRollupSource returns the source name for a given range. Exported for testing.
func SelectRollupSource(rangeWidth time.Duration) string {
	_, source := selectRollupTable(rangeWidth)
	return source
}

// labelFilter builds a JSONB containment clause for label filtering.
func labelFilter(labels string, argIdx int) (string, []interface{}) {
	labels = strings.TrimSpace(labels)
	if labels == "" {
		return "", nil
	}
	pairs := strings.Split(labels, ",")
	labelMap := map[string]string{}
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			labelMap[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	if len(labelMap) == 0 {
		return "", nil
	}
	return fmt.Sprintf(" AND labels @> $%d::jsonb", argIdx), []interface{}{labelMap}
}
