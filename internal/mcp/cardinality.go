// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerCardinalityTools() {
	s.srv.AddTool(mcp.NewTool(
		"metric_cardinality",
		mcp.WithDescription("Show unique context, dimension, and instance counts per node. Helps understand metric volume and cardinality."),
		mcp.WithString("node_id", mcp.Description("Filter by node ID. If omitted, shows all nodes.")),
		mcp.WithString("after", mcp.Description("Only count metrics collected after this time. Accepts relative like '-24h' or ISO timestamp.")),
	), s.handleCardinality)
}

type cardinalityResult struct {
	NodeID          string `json:"node_id"`
	UniqueContexts  int    `json:"unique_contexts"`
	UniqueDimensions int   `json:"unique_dimensions"`
	UniqueInstances int    `json:"unique_instances"`
	TotalSamples    int64  `json:"total_samples"`
}

func (s *Server) handleCardinality(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)

	afterTime := time.Now().UTC().Add(-24 * time.Hour)
	if afterStr != "" {
		t, err := parseTimeArg(afterStr)
		if err != nil {
			return textResult(fmt.Sprintf("Invalid 'after': %v", err)), nil
		}
		afterTime = t
	}

	query := `
		SELECT
			node_id,
			COUNT(DISTINCT context) AS unique_contexts,
			COUNT(DISTINCT dimension) AS unique_dimensions,
			COUNT(DISTINCT COALESCE(instance, '')) AS unique_instances,
			COUNT(*) AS total_samples
		FROM hardware_metric_samples
		WHERE collected_at >= $1
	`
	args := []interface{}{afterTime}
	argIdx := 2

	if nodeID != "" {
		query += fmt.Sprintf(" AND node_id = $%d", argIdx)
		args = append(args, nodeID)
		argIdx++
	}
	query += " GROUP BY node_id ORDER BY node_id"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error: %v", err)), nil
	}
	defer rows.Close()

	var results []cardinalityResult
	for rows.Next() {
		var r cardinalityResult
		if err := rows.Scan(&r.NodeID, &r.UniqueContexts, &r.UniqueDimensions, &r.UniqueInstances, &r.TotalSamples); err != nil {
			return textResult(fmt.Sprintf("Error scanning: %v", err)), nil
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		return textResult("No metric data found in the specified time range."), nil
	}

	return jsonResult(results)
}
