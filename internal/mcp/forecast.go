// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerForecastTools() {
	s.srv.AddTool(mcp.NewTool(
		"forecast_capacity",
		mcp.WithDescription("Predict when a metric will hit a threshold using linear regression on historical data. Uses rollup tables for efficiency."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID to forecast.")),
		mcp.WithString("context", mcp.Required(), mcp.Description("Metric context (e.g. 'system.cpu', 'disk.space').")),
		mcp.WithString("dimension", mcp.Description("Dimension to forecast. If omitted, uses all dimensions summed.")),
		mcp.WithString("after", mcp.Description("Historical data start. Default '-7d'.")),
		mcp.WithNumber("threshold", mcp.Required(), mcp.Description("Target threshold value to predict crossing time for.")),
	), s.handleForecast)
}

type forecastResult struct {
	NodeID       string     `json:"node_id"`
	Context      string     `json:"context"`
	Dimension    string     `json:"dimension,omitempty"`
	CurrentValue float64    `json:"current_value"`
	Trend        float64    `json:"trend_per_hour"`
	Threshold    float64    `json:"threshold"`
	HitsAt       *time.Time `json:"predicted_threshold_hit,omitempty"`
	HoursUntil   *float64   `json:"hours_until_threshold,omitempty"`
	DataPoints   int        `json:"data_points_used"`
	R2           float64    `json:"r_squared"`
	Message      string     `json:"message"`
}

func (s *Server) handleForecast(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	nodeID, _ := req.Params.Arguments["node_id"].(string)
	ctxFilter, _ := req.Params.Arguments["context"].(string)
	dimension, _ := req.Params.Arguments["dimension"].(string)
	afterStr, _ := req.Params.Arguments["after"].(string)
	threshold, _ := req.Params.Arguments["threshold"].(float64)

	if afterStr == "" {
		afterStr = "-7d"
	}
	afterTime, err := parseTimeArg(afterStr)
	if err != nil {
		return textResult(fmt.Sprintf("Invalid 'after': %v", err)), nil
	}

	query := `
		SELECT bucket, avg_value
		FROM hardware_metric_rollups_1h
		WHERE node_id = $1 AND context = $2 AND bucket >= $3
	`
	args := []interface{}{nodeID, ctxFilter, afterTime}
	argIdx := 4

	if dimension != "" {
		query += fmt.Sprintf(" AND dimension = $%d", argIdx)
		args = append(args, dimension)
		argIdx++
	}
	query += " ORDER BY bucket ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return textResult(fmt.Sprintf("Error: %v", err)), nil
	}
	defer rows.Close()

	type point struct {
		t time.Time
		v float64
	}
	var points []point
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.t, &p.v); err != nil {
			continue
		}
		points = append(points, p)
	}

	if len(points) < 3 {
		return jsonResult(forecastResult{
			NodeID:     nodeID,
			Context:    ctxFilter,
			Dimension:  dimension,
			Threshold:  threshold,
			DataPoints: len(points),
			Message:    "Insufficient data points for forecast (need at least 3).",
		})
	}

	baseTime := points[0].t
	xs := make([]float64, len(points))
	ys := make([]float64, len(points))
	for i, p := range points {
		xs[i] = p.t.Sub(baseTime).Hours()
		ys[i] = p.v
	}

	slope, intercept, r2 := linearRegression(xs, ys)

	currentVal := ys[len(ys)-1]
	currentHours := xs[len(xs)-1]

	result := forecastResult{
		NodeID:       nodeID,
		Context:      ctxFilter,
		Dimension:    dimension,
		CurrentValue: round2(currentVal),
		Trend:        round2(slope),
		Threshold:    threshold,
		DataPoints:   len(points),
		R2:           round2(r2),
	}

	if slope == 0 {
		result.Message = "Metric is flat — no trend detected."
		return jsonResult(result)
	}

	hoursToThreshold := (threshold - (intercept + slope*currentHours)) / slope
	if hoursToThreshold <= 0 {
		if currentVal >= threshold {
			result.Message = "Metric already exceeds threshold."
		} else {
			result.Message = "Trend is moving away from threshold."
		}
	} else {
		hitsAt := time.Now().UTC().Add(time.Duration(hoursToThreshold * float64(time.Hour)))
		result.HitsAt = &hitsAt
		h := round2(hoursToThreshold)
		result.HoursUntil = &h
		if hoursToThreshold < 24 {
			result.Message = fmt.Sprintf("WARNING: Threshold will be reached in ~%.1f hours.", hoursToThreshold)
		} else {
			result.Message = fmt.Sprintf("Threshold predicted in ~%.0f hours (~%.1f days).", hoursToThreshold, hoursToThreshold/24)
		}
	}

	return jsonResult(result)
}

func linearRegression(xs, ys []float64) (slope, intercept, r2 float64) {
	n := float64(len(xs))
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumX2 += xs[i] * xs[i]
		sumY2 += ys[i] * ys[i]
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n, 0
	}

	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	meanY := sumY / n
	var ssTot, ssRes float64
	for i := range xs {
		predicted := slope*xs[i] + intercept
		ssRes += (ys[i] - predicted) * (ys[i] - predicted)
		ssTot += (ys[i] - meanY) * (ys[i] - meanY)
	}

	if ssTot == 0 {
		r2 = 1.0
	} else {
		r2 = math.Max(0, 1-ssRes/ssTot)
	}

	return slope, intercept, r2
}
