// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerAnomalyTools() {
	s.srv.AddTool(mcp.NewTool(
		"detect_anomalies",
		mcp.WithDescription("Detect statistical anomalies in metric streams using z-score analysis. Returns dimensions with values significantly outside normal range."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID to analyze.")),
		mcp.WithString("context", mcp.Description("Filter by metric context. If omitted, checks all contexts.")),
		mcp.WithString("after", mcp.Description("Analysis window start. Default '-1h'.")),
		mcp.WithNumber("threshold", mcp.Description("Z-score threshold for anomaly. Default 2.5.")),
		mcp.WithNumber("limit", mcp.Description("Max anomalies to return. Default 20.")),
	), s.handleDetectAnomalies)
}

type anomalyResult struct {
	NodeID    string    `json:"node_id"`
	Anomalies []anomaly `json:"anomalies"`
	Total     int       `json:"total_dimensions_checked"`
}

type anomaly struct {
	Context   string  `json:"context"`
	Dimension string  `json:"dimension"`
	Instance  string  `json:"instance,omitempty"`
	LatestVal float64 `json:"latest_value"`
	Mean      float64 `json:"mean"`
	StdDev    float64 `json:"std_dev"`
	ZScore    float64 `json:"z_score"`
	Direction string  `json:"direction"`
}

func (s *Server) handleDetectAnomalies(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	ctxFilter, _ := req.Params.Arguments["context"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)
	thresholdF, _ := req.Params.Arguments["threshold"].(float64)
	limitF, _ := req.Params.Arguments["limit"].(float64)

	if afterStr == "" {
		afterStr = "-1h"
	}
	threshold := 2.5
	if thresholdF > 0 {
		threshold = thresholdF
	}
	limit := 20
	if limitF > 0 {
		limit = int(limitF)
	}

	afterTime, err := parseTimeArg(afterStr)
	if err != nil {
		return textResult(fmt.Sprintf("Invalid 'after': %v", err)), nil
	}

	query := `
		SELECT context, dimension, COALESCE(instance, '') as instance,
			AVG(value) as mean,
			STDDEV_POP(value) as stddev,
			(array_agg(value ORDER BY collected_at DESC))[1] as latest
		FROM hardware_metric_samples
		WHERE node_id = $1 AND collected_at >= $2
	`
	args := []interface{}{nodeID, afterTime}
	argIdx := 3

	if ctxFilter != "" {
		query += fmt.Sprintf(" AND context = $%d", argIdx)
		args = append(args, ctxFilter)
		argIdx++
	}

	query += ` GROUP BY context, dimension, COALESCE(instance, '')
		HAVING COUNT(*) >= 5 AND STDDEV_POP(value) > 0`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error: %v", err)), nil
	}
	defer rows.Close()

	var anomalies []anomaly
	totalChecked := 0
	for rows.Next() {
		totalChecked++
		var a anomaly
		var mean, stddev, latest float64
		if err := rows.Scan(&a.Context, &a.Dimension, &a.Instance, &mean, &stddev, &latest); err != nil {
			continue
		}

		zScore := (latest - mean) / stddev
		absZ := math.Abs(zScore)

		if absZ >= threshold {
			a.LatestVal = round2(latest)
			a.Mean = round2(mean)
			a.StdDev = round2(stddev)
			a.ZScore = round2(zScore)
			if zScore > 0 {
				a.Direction = "high"
			} else {
				a.Direction = "low"
			}
			anomalies = append(anomalies, a)
		}
	}

	sort.Slice(anomalies, func(i, j int) bool {
		return math.Abs(anomalies[i].ZScore) > math.Abs(anomalies[j].ZScore)
	})

	if len(anomalies) > limit {
		anomalies = anomalies[:limit]
	}

	return jsonResult(anomalyResult{
		NodeID:    nodeID,
		Anomalies: anomalies,
		Total:     totalChecked,
	})
}
