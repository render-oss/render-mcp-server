package metrics

import (
	"context"
	"fmt"

	"github.com/render-oss/render-mcp-server/pkg/client"
	metricstypes "github.com/render-oss/render-mcp-server/pkg/client/metrics"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(c *client.ClientWithResponses) *Repo {
	return &Repo{client: c}
}

type MetricType string

const (
	MetricTypeCPU           MetricType = "cpu"
	MetricTypeMemory        MetricType = "memory"
	MetricTypeHTTP          MetricType = "http"
	MetricTypeConnections   MetricType = "connections"
	MetricTypeInstanceCount MetricType = "instancecount"
	MetricTypeHTTPErrors    MetricType = "httperrors"
	MetricTypeResponseTime  MetricType = "responsetime"
)

type MetricsRequest struct {
	ResourceID        string
	MetricTypes       []MetricType
	StartTime         *client.StartTimeParam
	EndTime           *client.EndTimeParam
	Resolution        *float32
	AggregationMethod *metricstypes.ApplicationMetricAggregationMethod
}

type MetricData struct {
	Type MetricType                        `json:"type"`
	Data metricstypes.TimeSeriesCollection `json:"data"`
	Unit string                            `json:"unit"`
}

type MetricsResponse struct {
	ResourceID string `json:"resourceId"`
	TimeRange  struct {
		Start *client.StartTimeParam `json:"start,omitempty"`
		End   *client.EndTimeParam   `json:"end,omitempty"`
	} `json:"timeRange"`
	Metrics []MetricData `json:"metrics"`
}

func (r *Repo) GetMetrics(ctx context.Context, req MetricsRequest) (*MetricsResponse, error) {
	ownerId, err := session.FromContext(ctx).GetWorkspace(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	response := &MetricsResponse{
		ResourceID: req.ResourceID,
		Metrics:    []MetricData{},
	}

	response.TimeRange.Start = req.StartTime
	response.TimeRange.End = req.EndTime

	// Fetch metrics for each requested type
	for _, metricType := range req.MetricTypes {
		data, err := r.fetchMetric(ctx, ownerId, req.ResourceID, metricType, req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s metrics: %w", metricType, err)
		}
		response.Metrics = append(response.Metrics, data)
	}

	return response, nil
}

func (r *Repo) fetchMetric(ctx context.Context, ownerId, resourceId string, metricType MetricType, req MetricsRequest) (MetricData, error) {
	var data metricstypes.TimeSeriesCollection
	var unit string
	var err error

	switch metricType {
	case MetricTypeCPU:
		data, unit, err = r.getCPUMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeMemory:
		data, unit, err = r.getMemoryMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeHTTP:
		data, unit, err = r.getHTTPMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeConnections:
		data, unit, err = r.getConnectionsMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeInstanceCount:
		data, unit, err = r.getInstanceCountMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeHTTPErrors:
		data, unit, err = r.getHTTPErrorMetrics(ctx, ownerId, resourceId, req)
	case MetricTypeResponseTime:
		data, unit, err = r.getResponseTimeMetrics(ctx, ownerId, resourceId, req)
	default:
		return MetricData{}, fmt.Errorf("unsupported metric type: %s", metricType)
	}

	if err != nil {
		return MetricData{}, err
	}

	return MetricData{
		Type: metricType,
		Data: data,
		Unit: unit,
	}, nil
}

func (r *Repo) getCPUMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetCpuParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	if req.AggregationMethod != nil {
		params.AggregationMethod = req.AggregationMethod
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetCpuWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get CPU metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, "", fmt.Errorf("empty response from CPU metrics API")
	}

	return *resp.JSON200, "percent", nil
}

func (r *Repo) getMemoryMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetMemoryParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// GetMemoryParams doesn't support AggregationMethod

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetMemoryWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get memory metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, "", fmt.Errorf("empty response from memory metrics API")
	}

	return *resp.JSON200, "bytes", nil
}

func (r *Repo) getHTTPMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {

	params := &client.GetHttpRequestsParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// GetHttpRequestsParams doesn't support AggregationMethod

	// Use ServiceResourceQueryParam for HTTP requests
	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	resp, err := r.client.GetHttpRequestsWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get HTTP metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("HTTP metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, "requests", nil
	}

	return *resp.JSON200, "requests", nil
}

func (r *Repo) getConnectionsMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetActiveConnectionsParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// GetActiveConnectionsParams doesn't support AggregationMethod

	// Use DatastoreResourceQueryParam for active connections
	datastoreResource := metricstypes.DatastoreResourceQueryParam(resourceId)
	params.Resource = &datastoreResource

	resp, err := r.client.GetActiveConnectionsWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("connections metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, "connections", nil
	}

	return *resp.JSON200, "connections", nil
}

func (r *Repo) getInstanceCountMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetInstanceCountParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetInstanceCountWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get instance count metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("instance count metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, "instances", nil
	}

	return *resp.JSON200, "instances", nil
}

func (r *Repo) getHTTPErrorMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetHttpRequestsParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Use ServiceResourceQueryParam for HTTP requests
	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	// Aggregate by status code to get error breakdown
	aggregateBy := metricstypes.HttpAggregateByStatusCode
	params.AggregateBy = &aggregateBy

	resp, err := r.client.GetHttpRequestsWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get HTTP error metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("HTTP error metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, "errors_per_minute", nil
	}

	return *resp.JSON200, "errors_per_minute", nil
}

func (r *Repo) getResponseTimeMetrics(ctx context.Context, ownerId, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, string, error) {
	params := &client.GetHttpLatencyParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Use ServiceResourceQueryParam for HTTP latency requests
	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	// Get P50, P95, P99 percentiles by making a single request with P95 as representative
	// Note: The API might return multiple percentiles, but we'll request P95 as a good middle ground
	quantile := metricstypes.Quantile(0.95)
	params.Quantile = &quantile

	resp, err := r.client.GetHttpLatencyWithResponse(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get response time metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("response time metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, "milliseconds", nil
	}

	return *resp.JSON200, "milliseconds", nil
}
