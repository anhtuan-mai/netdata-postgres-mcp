// SPDX-License-Identifier: GPL-3.0-or-later

// Package collector queries Netdata's HTTP API and converts responses into
// MetricSample records ready for PostgreSQL insertion.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"
)

// Collector fetches metrics from a Netdata agent/parent via HTTP API.
type Collector struct {
	baseURL   string
	apiKey    string
	nodeID    string
	contexts  []string
	interval  int
	client    *http.Client
	logger    *slog.Logger
}

// New creates a Collector targeting the given Netdata instance.
func New(baseURL, apiKey, nodeID string, contexts []string, intervalSec int, logger *slog.Logger) *Collector {
	return &Collector{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		nodeID:   nodeID,
		contexts: contexts,
		interval: intervalSec,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
	}
}

// ResolveNodeID determines the node identifier. If configured, it returns
// that value. Otherwise it queries Netdata's /api/v1/info endpoint, and
// falls back to the OS hostname.
func (c *Collector) ResolveNodeID(ctx context.Context) (string, error) {
	if c.nodeID != "" {
		return c.nodeID, nil
	}

	// Try Netdata info endpoint
	infoURL := c.baseURL + "/api/v1/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err == nil {
		c.setAuth(req)
		resp, err := c.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var info struct {
					Hostname string `json:"hostname"`
					UID      string `json:"uid"`
				}
				if json.NewDecoder(resp.Body).Decode(&info) == nil {
					if info.UID != "" {
						c.nodeID = info.UID
						return c.nodeID, nil
					}
					if info.Hostname != "" {
						c.nodeID = info.Hostname
						return c.nodeID, nil
					}
				}
			}
		}
	}

	// Fall back to OS hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("resolving node ID: %w", err)
	}
	c.nodeID = hostname
	return c.nodeID, nil
}

// Collect fetches current metrics from Netdata and returns them as samples.
// It tries the JSON /api/v3/data endpoint first. If that fails or returns
// unparseable data, it falls back to the Prometheus allmetrics endpoint.
func (c *Collector) Collect(ctx context.Context) ([]store.MetricSample, error) {
	samples, err := c.collectJSON(ctx)
	if err != nil {
		c.logger.Warn("JSON API failed, falling back to Prometheus", "error", err)
		return c.collectPrometheus(ctx)
	}
	if len(samples) == 0 {
		c.logger.Warn("JSON API returned no samples, trying Prometheus fallback")
		return c.collectPrometheus(ctx)
	}
	return samples, nil
}

// netdataV3Response represents the Netdata /api/v3/data JSON response.
// The response shape uses parallel arrays: "labels" holds column names,
// and "result" contains data rows keyed by a composite label string.
type netdataV3Response struct {
	Labels []string                   `json:"labels"`
	Result map[string]json.RawMessage `json:"result"`
	// v3 data endpoint can also return a flat structure
	Data [][]interface{} `json:"data"`
}

// collectJSON queries /api/v3/data with group_by=dimension,node,instance.
func (c *Collector) collectJSON(ctx context.Context) ([]store.MetricSample, error) {
	contextsParam := strings.Join(c.contexts, ",")
	afterParam := fmt.Sprintf("-%d", c.interval)

	u := fmt.Sprintf("%s/api/v3/data?contexts=%s&after=%s&points=1&time_group=avg&group_by=dimension,node,instance&format=json",
		c.baseURL,
		url.QueryEscape(contextsParam),
		afterParam,
	)

	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("querying JSON API: %w", err)
	}

	return c.parseJSONResponse(body, time.Now().UTC())
}

// parseJSONResponse parses the Netdata v3 data response body into samples.
// Exported for testing.
func (c *Collector) parseJSONResponse(body []byte, collectedAt time.Time) ([]store.MetricSample, error) {
	// Netdata v3/data with group_by returns a JSON object with a "result" map.
	// Each key in "result" is a composite like "context,dimension,node,instance"
	// and the value is a nested structure with actual metric values.
	//
	// However, the exact shape varies by Netdata version. We try multiple
	// parsing strategies.

	var samples []store.MetricSample

	// Strategy 1: Try the "result" map format (group_by response)
	var grouped struct {
		Result map[string]struct {
			Labels map[string]string `json:"labels"`
			Point  []interface{}     `json:"point"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &grouped); err == nil && len(grouped.Result) > 0 {
		for key, entry := range grouped.Result {
			if len(entry.Point) == 0 {
				continue
			}
			val, ok := toFloat64(entry.Point[0])
			if !ok {
				continue
			}

			parts := strings.SplitN(key, ",", 4)
			sample := store.MetricSample{
				NodeID:      c.nodeID,
				CollectedAt: collectedAt,
				Value:       val,
				Labels:      entry.Labels,
			}
			if len(parts) >= 1 {
				sample.Context = strings.TrimSpace(parts[0])
			}
			if len(parts) >= 2 {
				sample.Dimension = strings.TrimSpace(parts[1])
			}
			if len(parts) >= 3 {
				// node label, skip — we use our configured nodeID
			}
			if len(parts) >= 4 {
				sample.Instance = strings.TrimSpace(parts[3])
			}
			if sample.Context != "" && sample.Dimension != "" {
				samples = append(samples, sample)
			}
		}
		return samples, nil
	}

	// Strategy 2: Try flat "data" array with "labels" columns
	var flat struct {
		Labels []string        `json:"labels"`
		Data   [][]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && len(flat.Data) > 0 && len(flat.Labels) > 0 {
		// First label is usually "time", rest are dimension names
		for _, row := range flat.Data {
			if len(row) < 2 {
				continue
			}
			// row[0] is timestamp
			ts := collectedAt
			if tsFloat, ok := toFloat64(row[0]); ok && tsFloat > 0 {
				ts = time.Unix(int64(tsFloat), 0).UTC()
			}
			for i := 1; i < len(row) && i < len(flat.Labels); i++ {
				val, ok := toFloat64(row[i])
				if !ok {
					continue
				}
				samples = append(samples, store.MetricSample{
					NodeID:      c.nodeID,
					CollectedAt: ts,
					Context:     "unknown",
					Dimension:   flat.Labels[i],
					Value:       val,
				})
			}
		}
		return samples, nil
	}

	// Strategy 3: Try fully generic JSON — array of objects
	var objects []map[string]interface{}
	if err := json.Unmarshal(body, &objects); err == nil && len(objects) > 0 {
		for _, obj := range objects {
			sample := store.MetricSample{
				NodeID:      c.nodeID,
				CollectedAt: collectedAt,
			}
			if v, ok := obj["context"].(string); ok {
				sample.Context = v
			}
			if v, ok := obj["dimension"].(string); ok {
				sample.Dimension = v
			}
			if v, ok := obj["instance"].(string); ok {
				sample.Instance = v
			}
			if v, ok := obj["chart"].(string); ok {
				sample.Chart = v
			}
			if v, ok := obj["family"].(string); ok {
				sample.Family = v
			}
			if v, ok := obj["unit"].(string); ok {
				sample.Unit = v
			}
			if v, ok := toFloat64(obj["value"]); ok {
				sample.Value = v
			}
			if sample.Context != "" && sample.Dimension != "" {
				samples = append(samples, sample)
			}
		}
		return samples, nil
	}

	return nil, fmt.Errorf("could not parse JSON response (body length: %d)", len(body))
}

// collectPrometheus queries /api/v3/allmetrics?format=prometheus and parses
// the text-based Prometheus exposition format.
func (c *Collector) collectPrometheus(ctx context.Context) ([]store.MetricSample, error) {
	u := fmt.Sprintf("%s/api/v3/allmetrics?format=prometheus&source=average", c.baseURL)

	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("querying Prometheus API: %w", err)
	}

	return c.parsePrometheusResponse(body, time.Now().UTC())
}

// parsePrometheusResponse parses Prometheus exposition format text into samples.
// Exported for testing.
func (c *Collector) parsePrometheusResponse(body []byte, collectedAt time.Time) ([]store.MetricSample, error) {
	lines := strings.Split(string(body), "\n")
	var samples []store.MetricSample
	enabledSet := make(map[string]bool, len(c.contexts))
	for _, ctx := range c.contexts {
		// Netdata prometheus format uses underscores: system.cpu -> netdata_system_cpu
		normalized := "netdata_" + strings.ReplaceAll(ctx, ".", "_")
		enabledSet[normalized] = true
		// Also allow the raw context name for matching
		enabledSet[ctx] = true
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		sample, ok := parsePrometheusLine(line, c.nodeID, collectedAt)
		if !ok {
			continue
		}

		// Filter to enabled contexts: check if the metric name prefix matches.
		matched := false
		for prefix := range enabledSet {
			if strings.HasPrefix(sample.Context, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		samples = append(samples, sample)
	}

	return samples, nil
}

// parsePrometheusLine parses a single Prometheus exposition line into a MetricSample.
// Format: metric_name{label="value",...} value [timestamp]
func parsePrometheusLine(line, nodeID string, collectedAt time.Time) (store.MetricSample, bool) {
	sample := store.MetricSample{
		NodeID:      nodeID,
		CollectedAt: collectedAt,
		Labels:      map[string]string{},
	}

	// Split metric name from labels and value
	labelStart := strings.IndexByte(line, '{')
	labelEnd := strings.IndexByte(line, '}')

	var metricName string
	var valueStr string

	if labelStart >= 0 && labelEnd > labelStart {
		metricName = line[:labelStart]
		labelsStr := line[labelStart+1 : labelEnd]
		valueStr = strings.TrimSpace(line[labelEnd+1:])

		// Parse labels
		for _, pair := range splitLabels(labelsStr) {
			eqIdx := strings.IndexByte(pair, '=')
			if eqIdx < 0 {
				continue
			}
			key := strings.TrimSpace(pair[:eqIdx])
			val := strings.Trim(strings.TrimSpace(pair[eqIdx+1:]), "\"")
			sample.Labels[key] = val

			switch key {
			case "chart":
				sample.Chart = val
			case "family":
				sample.Family = val
			case "dimension":
				sample.Dimension = val
			case "instance":
				sample.Instance = val
			case "units":
				sample.Unit = val
			}
		}
	} else {
		// No labels
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return sample, false
		}
		metricName = parts[0]
		valueStr = parts[1]
	}

	// The Netdata Prometheus metric name encodes the context:
	// netdata_system_cpu_percentage_average{...} -> context = system.cpu
	sample.Context = prometheusNameToContext(metricName)

	if sample.Dimension == "" {
		// Use the metric name suffix as dimension if not in labels
		sample.Dimension = metricName
	}

	// Parse value
	val, ok := parseFloat(valueStr)
	if !ok {
		return sample, false
	}
	sample.Value = val

	return sample, true
}

// prometheusNameToContext converts a Netdata prometheus metric name to context.
// E.g., "netdata_system_cpu_percentage_average" -> "netdata_system_cpu"
func prometheusNameToContext(name string) string {
	// Remove common suffixes added by Netdata
	for _, suffix := range []string{"_average", "_total", "_sum"} {
		name = strings.TrimSuffix(name, suffix)
	}
	// Remove unit suffixes like _percentage, _bytes, _kilobytes, etc.
	for _, suffix := range []string{
		"_percentage", "_bytes", "_kilobytes", "_megabytes",
		"_seconds", "_operations", "_requests", "_count",
	} {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	return name
}

// splitLabels splits a Prometheus label string respecting quoted values.
func splitLabels(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, ch := range s {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			current.WriteRune(ch)
			continue
		}
		if ch == '"' {
			inQuote = !inQuote
			current.WriteRune(ch)
			continue
		}
		if ch == ',' && !inQuote {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// doGet performs an authenticated GET request and returns the response body.
func (c *Collector) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// setAuth adds authentication headers if an API key is configured.
func (c *Collector) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// toFloat64 converts a JSON numeric value to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// parseFloat parses a Prometheus-style float value.
func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	// Handle Prometheus special values
	switch s {
	case "+Inf", "Inf":
		return 0, false // skip infinities
	case "-Inf":
		return 0, false
	case "NaN":
		return 0, false
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err == nil
}
