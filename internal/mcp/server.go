// SPDX-License-Identifier: GPL-3.0-or-later

// Package mcp implements an MCP (Model Context Protocol) server that exposes
// hardware metrics stored in PostgreSQL to AI assistants.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/mark3labs/mcp-go/mcp"
)

// Server wraps an MCP server with access to the metrics database.
type Server struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	srv    *mcpserver.MCPServer
}

// New creates an MCP server with all hardware metric tools registered.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Server {
	s := &Server{
		pool:   pool,
		logger: logger,
	}

	srv := mcpserver.NewMCPServer(
		"netdata-postgres-mcp",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	// Register tools
	srv.AddTool(mcp.NewTool(
		"list_nodes",
		mcp.WithDescription("List monitored Netdata nodes stored in PostgreSQL. Returns node_id, hostname, netdata_base_url, and last_collected_at for each node."),
	), s.handleListNodes)

	srv.AddTool(mcp.NewTool(
		"latest_hardware_metrics",
		mcp.WithDescription("Return latest CPU, RAM, and disk metrics for a node. Results are grouped by context/instance/dimension."),
		mcp.WithString("node_id", mcp.Description("Node ID to query. If omitted, returns data for all nodes.")),
		mcp.WithString("contexts", mcp.Description("Comma-separated list of contexts to filter (e.g. 'system.cpu,system.ram'). If omitted, returns all contexts.")),
	), s.handleLatestMetrics)

	srv.AddTool(mcp.NewTool(
		"query_hardware_metrics",
		mcp.WithDescription("Query historical hardware metrics from PostgreSQL with flexible filtering."),
		mcp.WithString("node_id", mcp.Description("Filter by node ID.")),
		mcp.WithString("context", mcp.Description("Filter by metric context (e.g. 'system.cpu').")),
		mcp.WithString("dimension", mcp.Description("Filter by dimension (e.g. 'user', 'system').")),
		mcp.WithString("instance", mcp.Description("Filter by instance (e.g. disk name, mount point).")),
		mcp.WithString("after", mcp.Description("Start time as ISO timestamp or relative duration like '-1h', '-30m'.")),
		mcp.WithString("before", mcp.Description("End time as ISO timestamp.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results. Default 500.")),
	), s.handleQueryMetrics)

	srv.AddTool(mcp.NewTool(
		"summarize_hardware_performance",
		mcp.WithDescription("Summarize hardware performance for a node over a time window. Provides CPU/RAM/disk averages, peaks, and a human-readable summary."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID to summarize.")),
		mcp.WithString("after", mcp.Description("Start of time window. Default '-1h'. Accepts ISO timestamp or relative duration.")),
		mcp.WithString("before", mcp.Description("End of time window. Accepts ISO timestamp. Default is now.")),
	), s.handleSummarize)

	srv.AddTool(mcp.NewTool(
		"find_hardware_bottlenecks",
		mcp.WithDescription("Detect likely CPU/RAM/disk bottlenecks from stored metric samples. Returns bottleneck type, evidence, confidence score, and explanation."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID to analyze.")),
		mcp.WithString("after", mcp.Description("Start of analysis window. Default '-15m'. Accepts relative duration or ISO timestamp.")),
	), s.handleBottlenecks)

	s.srv = srv
	s.registerAlertTools()
	return s
}

// MCPServer returns the underlying mcp-go server for transport setup.
func (s *Server) MCPServer() *mcpserver.MCPServer {
	return s.srv
}

// --- Tool Handlers ---

// withTimeout wraps a context with a 30-second deadline for database queries.
func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}

func (s *Server) handleListNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT
			n.node_id,
			COALESCE(n.hostname, '') as hostname,
			COALESCE(n.netdata_base_url, '') as base_url,
			(SELECT MAX(collected_at) FROM hardware_metric_samples WHERE node_id = n.node_id) as last_collected
		FROM netdata_nodes n
		ORDER BY n.node_id
	`)
	if err != nil {
		return textResult(fmt.Sprintf("Error listing nodes: %v", err)), nil
	}
	defer rows.Close()

	type nodeRow struct {
		NodeID         string     `json:"node_id"`
		Hostname       string     `json:"hostname"`
		NetdataBaseURL string     `json:"netdata_base_url"`
		LastCollected  *time.Time `json:"last_collected_at"`
	}

	var nodes []nodeRow
	for rows.Next() {
		var n nodeRow
		if err := rows.Scan(&n.NodeID, &n.Hostname, &n.NetdataBaseURL, &n.LastCollected); err != nil {
			return textResult(fmt.Sprintf("Error scanning node: %v", err)), nil
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return textResult(fmt.Sprintf("Error iterating nodes: %v", err)), nil
	}

	return jsonResult(nodes)
}

func (s *Server) handleLatestMetrics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	contextsStr, _ := req.Params.Arguments["contexts"].(string)

	query := `
		SELECT node_id, collected_at, context, chart, family, instance, dimension, unit, value
		FROM hardware_latest_metrics
		WHERE 1=1
	`
	var args []interface{}
	argIdx := 1

	if nodeID != "" {
		query += fmt.Sprintf(" AND node_id = $%d", argIdx)
		args = append(args, nodeID)
		argIdx++
	}
	if contextsStr != "" {
		ctxList := strings.Split(contextsStr, ",")
		for i := range ctxList {
			ctxList[i] = strings.TrimSpace(ctxList[i])
		}
		query += fmt.Sprintf(" AND context = ANY($%d)", argIdx)
		args = append(args, ctxList)
		argIdx++
	}
	query += " ORDER BY node_id, context, instance, dimension"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error querying latest metrics: %v", err)), nil
	}
	defer rows.Close()

	type metricRow struct {
		NodeID      string    `json:"node_id"`
		CollectedAt time.Time `json:"collected_at"`
		Context     string    `json:"context"`
		Chart       *string   `json:"chart,omitempty"`
		Family      *string   `json:"family,omitempty"`
		Instance    *string   `json:"instance,omitempty"`
		Dimension   string    `json:"dimension"`
		Unit        *string   `json:"unit,omitempty"`
		Value       float64   `json:"value"`
	}

	var metrics []metricRow
	for rows.Next() {
		var m metricRow
		if err := rows.Scan(&m.NodeID, &m.CollectedAt, &m.Context, &m.Chart, &m.Family, &m.Instance, &m.Dimension, &m.Unit, &m.Value); err != nil {
			return textResult(fmt.Sprintf("Error scanning metric: %v", err)), nil
		}
		metrics = append(metrics, m)
	}

	return jsonResult(metrics)
}

func (s *Server) handleQueryMetrics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	query := `
		SELECT node_id, collected_at, context, chart, family, instance, dimension, unit, value
		FROM hardware_metric_samples
		WHERE 1=1
	`
	var args []interface{}
	argIdx := 1

	if nodeID != "" {
		query += fmt.Sprintf(" AND node_id = $%d", argIdx)
		args = append(args, nodeID)
		argIdx++
	}
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
	if afterStr != "" {
		afterTime, err := parseTimeArg(afterStr)
		if err != nil {
			return textResult(fmt.Sprintf("Invalid 'after' value: %v", err)), nil
		}
		query += fmt.Sprintf(" AND collected_at >= $%d", argIdx)
		args = append(args, afterTime)
		argIdx++
	}
	if beforeStr != "" {
		beforeTime, err := parseTimeArg(beforeStr)
		if err != nil {
			return textResult(fmt.Sprintf("Invalid 'before' value: %v", err)), nil
		}
		query += fmt.Sprintf(" AND collected_at <= $%d", argIdx)
		args = append(args, beforeTime)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY collected_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error querying metrics: %v", err)), nil
	}
	defer rows.Close()

	type sampleRow struct {
		NodeID      string    `json:"node_id"`
		CollectedAt time.Time `json:"collected_at"`
		Context     string    `json:"context"`
		Chart       *string   `json:"chart,omitempty"`
		Family      *string   `json:"family,omitempty"`
		Instance    *string   `json:"instance,omitempty"`
		Dimension   string    `json:"dimension"`
		Unit        *string   `json:"unit,omitempty"`
		Value       float64   `json:"value"`
	}

	var results []sampleRow
	for rows.Next() {
		var r sampleRow
		if err := rows.Scan(&r.NodeID, &r.CollectedAt, &r.Context, &r.Chart, &r.Family, &r.Instance, &r.Dimension, &r.Unit, &r.Value); err != nil {
			return textResult(fmt.Sprintf("Error scanning row: %v", err)), nil
		}
		results = append(results, r)
	}

	return jsonResult(results)
}

func (s *Server) handleSummarize(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)
	beforeStr, _ := req.Params.Arguments["before"].(string)

	if afterStr == "" {
		afterStr = "-1h"
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

	// Query all samples in the window
	rows, err := s.pool.Query(ctx, `
		SELECT context, dimension, instance, value
		FROM hardware_metric_samples
		WHERE node_id = $1 AND collected_at >= $2 AND collected_at <= $3
		ORDER BY context, dimension
	`, nodeID, afterTime, beforeTime)
	if err != nil {
		return textResult(fmt.Sprintf("Error querying: %v", err)), nil
	}
	defer rows.Close()

	stats := map[string]map[string]*dimStats{} // context -> dimension -> stats
	for rows.Next() {
		var ctxName, dim string
		var inst *string
		var val float64
		if err := rows.Scan(&ctxName, &dim, &inst, &val); err != nil {
			continue
		}
		key := dim
		if inst != nil && *inst != "" {
			key = *inst + "/" + dim
		}
		if stats[ctxName] == nil {
			stats[ctxName] = map[string]*dimStats{}
		}
		ds := stats[ctxName][key]
		if ds == nil {
			ds = &dimStats{Min: math.MaxFloat64}
			stats[ctxName][key] = ds
		}
		ds.Sum += val
		ds.Count++
		if val > ds.Max {
			ds.Max = val
		}
		if val < ds.Min {
			ds.Min = val
		}
	}

	summary := buildSummary(nodeID, afterTime, beforeTime, stats)
	return jsonResult(summary)
}

// SummaryResult is the output of summarize_hardware_performance.
// Exported for testing.
type SummaryResult struct {
	NodeID    string    `json:"node_id"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	CPU       *ResourceSummary `json:"cpu,omitempty"`
	RAM       *ResourceSummary `json:"ram,omitempty"`
	DiskIO    *ResourceSummary `json:"disk_io,omitempty"`
	DiskUtil  *ResourceSummary `json:"disk_util,omitempty"`
	TopAbnormal []AbnormalDimension `json:"top_abnormal,omitempty"`
	Summary   string `json:"summary"`
}

// ResourceSummary holds average/peak for a resource category.
type ResourceSummary struct {
	AveragePercent float64            `json:"average_percent,omitempty"`
	PeakPercent    float64            `json:"peak_percent,omitempty"`
	Dimensions     map[string]DimStat `json:"dimensions,omitempty"`
}

// dimStats is internal per-dimension accumulator used during summarization.
type dimStats struct {
	Sum   float64
	Count int
	Max   float64
	Min   float64
}

// DimStat holds per-dimension statistics.
type DimStat struct {
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
	Min float64 `json:"min"`
}

// AbnormalDimension is a dimension with unusually high values.
type AbnormalDimension struct {
	Context   string  `json:"context"`
	Dimension string  `json:"dimension"`
	MaxValue  float64 `json:"max_value"`
	AvgValue  float64 `json:"avg_value"`
}

// BuildSummary creates a SummaryResult from collected stats. Exported for testing.
func BuildSummary(nodeID string, from, to time.Time, stats map[string]map[string]*DimStatsPublic) SummaryResult {
	internal := make(map[string]map[string]*dimStats)
	for ctx, dims := range stats {
		internal[ctx] = make(map[string]*dimStats)
		for dim, ds := range dims {
			internal[ctx][dim] = &dimStats{
				Sum:   ds.Sum,
				Count: ds.Count,
				Max:   ds.Max,
				Min:   ds.Min,
			}
		}
	}
	return buildSummary(nodeID, from, to, internal)
}

// DimStatsPublic is the public version of dimStats for testing.
type DimStatsPublic struct {
	Sum   float64
	Count int
	Max   float64
	Min   float64
}

func buildSummary(nodeID string, from, to time.Time, stats map[string]map[string]*dimStats) SummaryResult {
	result := SummaryResult{
		NodeID: nodeID,
		From:   from,
		To:     to,
	}

	var summaryParts []string

	// CPU summary
	if cpuDims, ok := stats["system.cpu"]; ok {
		cpu := &ResourceSummary{Dimensions: map[string]DimStat{}}
		var totalAvg, totalMax float64
		for dim, ds := range cpuDims {
			avg := ds.Sum / float64(ds.Count)
			cpu.Dimensions[dim] = DimStat{Avg: round2(avg), Max: round2(ds.Max), Min: round2(ds.Min)}
			if dim != "idle" {
				totalAvg += avg
				if ds.Max > totalMax {
					totalMax = ds.Max
				}
			}
		}
		cpu.AveragePercent = round2(totalAvg)
		cpu.PeakPercent = round2(totalMax)
		result.CPU = cpu
		summaryParts = append(summaryParts, fmt.Sprintf("CPU: %.1f%% avg, %.1f%% peak", totalAvg, totalMax))
	}

	// RAM summary
	if ramDims, ok := stats["system.ram"]; ok {
		ram := &ResourceSummary{Dimensions: map[string]DimStat{}}
		var usedAvg, usedMax float64
		for dim, ds := range ramDims {
			avg := ds.Sum / float64(ds.Count)
			ram.Dimensions[dim] = DimStat{Avg: round2(avg), Max: round2(ds.Max), Min: round2(ds.Min)}
			if dim == "used" {
				usedAvg = avg
				usedMax = ds.Max
			}
		}
		ram.AveragePercent = round2(usedAvg)
		ram.PeakPercent = round2(usedMax)
		result.RAM = ram
		summaryParts = append(summaryParts, fmt.Sprintf("RAM used: %.1f avg, %.1f peak (MiB)", usedAvg, usedMax))
	}

	// Disk IO summary
	for ctxName, dims := range stats {
		if !strings.HasPrefix(ctxName, "disk.io") && !strings.HasPrefix(ctxName, "system.io") {
			continue
		}
		dio := &ResourceSummary{Dimensions: map[string]DimStat{}}
		for dim, ds := range dims {
			avg := ds.Sum / float64(ds.Count)
			dio.Dimensions[dim] = DimStat{Avg: round2(avg), Max: round2(ds.Max), Min: round2(ds.Min)}
		}
		result.DiskIO = dio
		break
	}

	// Disk utilization
	for ctxName, dims := range stats {
		if !strings.HasPrefix(ctxName, "disk.util") {
			continue
		}
		du := &ResourceSummary{Dimensions: map[string]DimStat{}}
		var maxUtil float64
		for dim, ds := range dims {
			avg := ds.Sum / float64(ds.Count)
			du.Dimensions[dim] = DimStat{Avg: round2(avg), Max: round2(ds.Max), Min: round2(ds.Min)}
			if ds.Max > maxUtil {
				maxUtil = ds.Max
			}
		}
		du.PeakPercent = round2(maxUtil)
		result.DiskUtil = du
		summaryParts = append(summaryParts, fmt.Sprintf("Disk util peak: %.1f%%", maxUtil))
		break
	}

	// Top abnormal dimensions (highest max values, excluding idle/free)
	type abnormalCandidate struct {
		ctx string
		dim string
		max float64
		avg float64
	}
	var candidates []abnormalCandidate
	for ctxName, dims := range stats {
		for dim, ds := range dims {
			if dim == "idle" || dim == "free" || dim == "available" || dim == "cached" || dim == "buffers" {
				continue
			}
			avg := ds.Sum / float64(ds.Count)
			candidates = append(candidates, abnormalCandidate{ctx: ctxName, dim: dim, max: ds.Max, avg: avg})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].max > candidates[j].max })
	topN := 5
	if len(candidates) < topN {
		topN = len(candidates)
	}
	for _, c := range candidates[:topN] {
		result.TopAbnormal = append(result.TopAbnormal, AbnormalDimension{
			Context:   c.ctx,
			Dimension: c.dim,
			MaxValue:  round2(c.max),
			AvgValue:  round2(c.avg),
		})
	}

	if len(summaryParts) > 0 {
		result.Summary = fmt.Sprintf("Node %s (%s to %s): %s",
			nodeID, from.Format(time.RFC3339), to.Format(time.RFC3339),
			strings.Join(summaryParts, "; "))
	} else {
		result.Summary = fmt.Sprintf("Node %s: no CPU/RAM/disk data in the selected window", nodeID)
	}

	return result
}

func (s *Server) handleBottlenecks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)

	if afterStr == "" {
		afterStr = "-15m"
	}
	afterTime, err := parseTimeArg(afterStr)
	if err != nil {
		return textResult(fmt.Sprintf("Invalid 'after': %v", err)), nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT context, dimension, instance,
			AVG(value) as avg_val,
			MAX(value) as max_val,
			COUNT(*) as sample_count
		FROM hardware_metric_samples
		WHERE node_id = $1 AND collected_at >= $2
		GROUP BY context, dimension, instance
		ORDER BY context, dimension
	`, nodeID, afterTime)
	if err != nil {
		return textResult(fmt.Sprintf("Error: %v", err)), nil
	}
	defer rows.Close()

	var aggs []DimAgg
	for rows.Next() {
		var a DimAgg
		if err := rows.Scan(&a.Context, &a.Dimension, &a.Instance, &a.Avg, &a.Max, &a.Count); err != nil {
			continue
		}
		aggs = append(aggs, a)
	}

	result := DetectBottlenecks(nodeID, aggs)
	return jsonResult(result)
}

// BottleneckResult is the output of find_hardware_bottlenecks. Exported for testing.
type BottleneckResult struct {
	NodeID         string             `json:"node_id"`
	BottleneckType string             `json:"bottleneck_type"`
	Confidence     float64            `json:"confidence"`
	Evidence       []BottleneckEvidence `json:"evidence"`
	Explanation    string             `json:"explanation"`
}

// BottleneckEvidence is a supporting metric for a bottleneck detection.
type BottleneckEvidence struct {
	Context   string  `json:"context"`
	Dimension string  `json:"dimension"`
	Instance  string  `json:"instance,omitempty"`
	AvgValue  float64 `json:"avg_value"`
	MaxValue  float64 `json:"max_value"`
}

// DimAgg holds aggregated dimension data for bottleneck detection. Exported for testing.
type DimAgg struct {
	Context   string
	Dimension string
	Instance  *string
	Avg       float64
	Max       float64
	Count     int
}

// DetectBottlenecks analyzes aggregated metrics to find bottlenecks. Exported for testing.
func DetectBottlenecks(nodeID string, aggs []DimAgg) BottleneckResult {
	result := BottleneckResult{
		NodeID:         nodeID,
		BottleneckType: "none",
		Confidence:     0,
	}

	var cpuUsageAvg, cpuUsageMax float64
	var ramUsedAvg, ramUsedMax float64
	var diskUtilMax float64
	var iowaitAvg float64
	var swapUsedAvg float64
	var hasCPU, hasRAM, hasDisk bool

	for _, a := range aggs {
		inst := ""
		if a.Instance != nil {
			inst = *a.Instance
		}

		switch {
		case a.Context == "system.cpu":
			hasCPU = true
			if a.Dimension != "idle" {
				cpuUsageAvg += a.Avg
				if a.Max > cpuUsageMax {
					cpuUsageMax = a.Max
				}
			}
			if a.Dimension == "iowait" {
				iowaitAvg = a.Avg
			}
		case a.Context == "system.ram":
			hasRAM = true
			if a.Dimension == "used" {
				ramUsedAvg = a.Avg
				ramUsedMax = a.Max
			}
		case a.Context == "system.swap":
			if a.Dimension == "used" {
				swapUsedAvg = a.Avg
			}
		case strings.HasPrefix(a.Context, "disk.util"):
			hasDisk = true
			if a.Max > diskUtilMax {
				diskUtilMax = a.Max
			}
		}

		_ = inst // available for more detailed analysis
	}

	type candidate struct {
		btype      string
		confidence float64
		evidence   []BottleneckEvidence
		explain    string
	}
	var candidates []candidate

	// CPU bottleneck: high CPU usage (>80% avg or >95% peak)
	if hasCPU {
		if cpuUsageAvg > 80 || cpuUsageMax > 95 {
			conf := math.Min(1.0, (cpuUsageAvg/100.0+cpuUsageMax/100.0)/2*1.2)
			candidates = append(candidates, candidate{
				btype:      "cpu",
				confidence: round2(conf),
				evidence: []BottleneckEvidence{{
					Context: "system.cpu", Dimension: "total_usage",
					AvgValue: round2(cpuUsageAvg), MaxValue: round2(cpuUsageMax),
				}},
				explain: fmt.Sprintf("CPU usage avg %.1f%%, peak %.1f%% — indicates CPU saturation", cpuUsageAvg, cpuUsageMax),
			})
		}
	}

	// RAM bottleneck: very high used memory or swap in use
	if hasRAM {
		if ramUsedMax > 0 && (swapUsedAvg > 100 || ramUsedAvg > ramUsedMax*0.9) {
			conf := 0.6
			if swapUsedAvg > 500 {
				conf = 0.9
			}
			candidates = append(candidates, candidate{
				btype:      "ram",
				confidence: round2(conf),
				evidence: []BottleneckEvidence{
					{Context: "system.ram", Dimension: "used", AvgValue: round2(ramUsedAvg), MaxValue: round2(ramUsedMax)},
					{Context: "system.swap", Dimension: "used", AvgValue: round2(swapUsedAvg)},
				},
				explain: fmt.Sprintf("RAM used avg %.0f / max %.0f MiB, swap used avg %.0f MiB — memory pressure detected", ramUsedAvg, ramUsedMax, swapUsedAvg),
			})
		}
	}

	// Disk bottleneck: high utilization or high iowait
	if hasDisk || iowaitAvg > 5 {
		if diskUtilMax > 80 || iowaitAvg > 10 {
			conf := math.Min(1.0, (diskUtilMax/100.0+iowaitAvg/20.0)/2*1.3)
			ev := []BottleneckEvidence{}
			if hasDisk {
				ev = append(ev, BottleneckEvidence{Context: "disk.util", Dimension: "utilization", MaxValue: round2(diskUtilMax)})
			}
			if iowaitAvg > 0 {
				ev = append(ev, BottleneckEvidence{Context: "system.cpu", Dimension: "iowait", AvgValue: round2(iowaitAvg)})
			}
			candidates = append(candidates, candidate{
				btype:      "disk",
				confidence: round2(conf),
				evidence:   ev,
				explain:    fmt.Sprintf("Disk util peak %.1f%%, CPU iowait avg %.1f%% — disk I/O bottleneck likely", diskUtilMax, iowaitAvg),
			})
		}
	}

	if len(candidates) == 0 {
		result.BottleneckType = "none"
		result.Confidence = 0.9
		result.Explanation = "No bottlenecks detected — CPU, RAM, and disk metrics are within normal ranges"
		return result
	}

	// Pick highest confidence
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].confidence > candidates[j].confidence })
	best := candidates[0]
	result.BottleneckType = best.btype
	result.Confidence = best.confidence
	result.Evidence = best.evidence
	result.Explanation = best.explain

	return result
}

// --- Helpers ---

func textResult(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultText(msg)
}

func jsonResult(v interface{}) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult(fmt.Sprintf("Error marshaling JSON: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// parseTimeArg parses an ISO timestamp or a relative duration string like "-1h".
func parseTimeArg(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().UTC(), nil
	}

	// Relative duration: "-1h", "-30m", "-15m", "-24h"
	if strings.HasPrefix(s, "-") {
		dur, err := time.ParseDuration(s[1:]) // remove the leading "-"
		if err == nil {
			return time.Now().UTC().Add(-dur), nil
		}
		// Fall through to try ISO parsing
	}

	// Try common ISO formats
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse time %q: use ISO 8601 or relative like '-1h'", s)
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
