// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerNLQueryTools() {
	s.srv.AddTool(mcp.NewTool(
		"natural_query",
		mcp.WithDescription("Query metrics using natural language. Translates plain English to structured metric queries. Examples: 'CPU usage last hour', 'disk space past week', 'RAM trend last 3 days'."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural language query like 'show me CPU last hour' or 'disk usage past week'.")),
		mcp.WithString("node_id", mcp.Description("Node ID. If omitted, queries all nodes.")),
	), s.handleNaturalQuery)
}

type nlQueryPlan struct {
	OriginalQuery string `json:"original_query"`
	Interpretation string `json:"interpretation"`
	Context       string `json:"context"`
	Dimension     string `json:"dimension,omitempty"`
	After         string `json:"after"`
	NodeID        string `json:"node_id,omitempty"`
}

func (s *Server) handleNaturalQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	queryStr, _ := req.Params.Arguments["query"].(string)
	nodeID, _ := req.Params.Arguments["node_id"].(string)

	plan := parseNaturalQuery(queryStr, nodeID)

	fakeReq := mcp.CallToolRequest{}
	fakeReq.Params.Arguments = map[string]interface{}{
		"node_id": plan.NodeID,
		"context": plan.Context,
		"after":   plan.After,
	}
	if plan.Dimension != "" {
		fakeReq.Params.Arguments["dimension"] = plan.Dimension
	}

	result, err := s.handleQueryMetrics(ctx, fakeReq)
	if err != nil {
		return textResult(fmt.Sprintf("Error: %v", err)), nil
	}

	planJSON, _ := jsonResult(plan)
	combined := fmt.Sprintf("Query Plan:\n%s\n\nResults:\n%s",
		planJSON.Content[0].(mcp.TextContent).Text,
		result.Content[0].(mcp.TextContent).Text)

	return textResult(combined), nil
}

func parseNaturalQuery(query, nodeID string) nlQueryPlan {
	q := strings.ToLower(query)

	plan := nlQueryPlan{
		OriginalQuery: query,
		NodeID:        nodeID,
		After:         "-1h",
	}

	// Time extraction
	switch {
	case strings.Contains(q, "last week") || strings.Contains(q, "past week") || strings.Contains(q, "7 day"):
		plan.After = fmt.Sprintf("-%dh", 7*24)
	case strings.Contains(q, "last month") || strings.Contains(q, "past month") || strings.Contains(q, "30 day"):
		plan.After = fmt.Sprintf("-%dh", 30*24)
	case strings.Contains(q, "last 3 day") || strings.Contains(q, "past 3 day") || strings.Contains(q, "3 days"):
		plan.After = fmt.Sprintf("-%dh", 3*24)
	case strings.Contains(q, "last 24") || strings.Contains(q, "past 24") || strings.Contains(q, "yesterday"):
		plan.After = "-24h"
	case strings.Contains(q, "last 12") || strings.Contains(q, "past 12"):
		plan.After = "-12h"
	case strings.Contains(q, "last 6") || strings.Contains(q, "past 6"):
		plan.After = "-6h"
	case strings.Contains(q, "last 2 hour") || strings.Contains(q, "past 2 hour"):
		plan.After = "-2h"
	case strings.Contains(q, "last 30") || strings.Contains(q, "past 30"):
		plan.After = "-30m"
	case strings.Contains(q, "last 15") || strings.Contains(q, "past 15") || strings.Contains(q, "15 min"):
		plan.After = "-15m"
	case strings.Contains(q, "last 5") || strings.Contains(q, "past 5") || strings.Contains(q, "5 min"):
		plan.After = "-5m"
	}

	// Context extraction
	switch {
	case strings.Contains(q, "cpu"):
		plan.Context = "system.cpu"
		plan.Interpretation = "CPU usage metrics"
		if strings.Contains(q, "user") {
			plan.Dimension = "user"
		} else if strings.Contains(q, "system") || strings.Contains(q, "kernel") {
			plan.Dimension = "system"
		} else if strings.Contains(q, "iowait") || strings.Contains(q, "io wait") {
			plan.Dimension = "iowait"
		}
	case strings.Contains(q, "ram") || strings.Contains(q, "memory") || strings.Contains(q, "mem"):
		plan.Context = "system.ram"
		plan.Interpretation = "RAM usage metrics"
		if strings.Contains(q, "used") {
			plan.Dimension = "used"
		} else if strings.Contains(q, "free") {
			plan.Dimension = "free"
		}
	case strings.Contains(q, "swap"):
		plan.Context = "system.swap"
		plan.Interpretation = "Swap usage metrics"
	case strings.Contains(q, "disk space") || strings.Contains(q, "storage") || strings.Contains(q, "disk.space"):
		plan.Context = "disk.space"
		plan.Interpretation = "Disk space metrics"
	case strings.Contains(q, "disk io") || strings.Contains(q, "disk read") || strings.Contains(q, "disk write"):
		plan.Context = "disk.io"
		plan.Interpretation = "Disk I/O throughput"
		if strings.Contains(q, "read") {
			plan.Dimension = "reads"
		} else if strings.Contains(q, "write") {
			plan.Dimension = "writes"
		}
	case strings.Contains(q, "disk util") || strings.Contains(q, "disk busy"):
		plan.Context = "disk.util"
		plan.Interpretation = "Disk utilization percentage"
	case strings.Contains(q, "disk"):
		plan.Context = "disk.io"
		plan.Interpretation = "Disk I/O metrics"
	case strings.Contains(q, "network") || strings.Contains(q, "net") || strings.Contains(q, "bandwidth"):
		plan.Context = "system.ip"
		plan.Interpretation = "Network IP traffic"
	case strings.Contains(q, "io") || strings.Contains(q, "i/o"):
		plan.Context = "system.io"
		plan.Interpretation = "System I/O metrics"
	default:
		plan.Context = "system.cpu"
		plan.Interpretation = "Defaulting to CPU metrics (could not determine specific context)"
	}

	// Build final interpretation
	afterDur, _ := parseTimeArg(plan.After)
	window := time.Since(afterDur).Round(time.Minute)
	plan.Interpretation = fmt.Sprintf("%s over the last %s", plan.Interpretation, window)

	return plan
}
