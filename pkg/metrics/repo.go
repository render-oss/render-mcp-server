package metrics

import (
	"context"
	"fmt"

	"github.com/render-oss/render-mcp-server/pkg/client"
	metricstypes "github.com/render-oss/render-mcp-server/pkg/client/metrics"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

// MetricsClient interface for dependency injection in testing
type MetricsClient interface {
	GetCpuWithResponse(ctx context.Context, params *client.GetCpuParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuResponse, error)
	GetMemoryWithResponse(ctx context.Context, params *client.GetMemoryParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryResponse, error)
	GetHttpRequestsWithResponse(ctx context.Context, params *client.GetHttpRequestsParams, reqEditors ...client.RequestEditorFn) (*client.GetHttpRequestsResponse, error)
	GetHttpLatencyWithResponse(ctx context.Context, params *client.GetHttpLatencyParams, reqEditors ...client.RequestEditorFn) (*client.GetHttpLatencyResponse, error)
	GetActiveConnectionsWithResponse(ctx context.Context, params *client.GetActiveConnectionsParams, reqEditors ...client.RequestEditorFn) (*client.GetActiveConnectionsResponse, error)
	GetInstanceCountWithResponse(ctx context.Context, params *client.GetInstanceCountParams, reqEditors ...client.RequestEditorFn) (*client.GetInstanceCountResponse, error)
	GetCpuLimitWithResponse(ctx context.Context, params *client.GetCpuLimitParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuLimitResponse, error)
	GetCpuTargetWithResponse(ctx context.Context, params *client.GetCpuTargetParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuTargetResponse, error)
	GetMemoryLimitWithResponse(ctx context.Context, params *client.GetMemoryLimitParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryLimitResponse, error)
	GetMemoryTargetWithResponse(ctx context.Context, params *client.GetMemoryTargetParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryTargetResponse, error)
	GetBandwidthWithResponse(ctx context.Context, params *client.GetBandwidthParams, reqEditors ...client.RequestEditorFn) (*client.GetBandwidthResponse, error)
}

type Repo struct {
	client MetricsClient
}

func NewRepo(c MetricsClient) *Repo {
	return &Repo{client: c}
}

type MetricType string

const (
	MetricTypeCPUUsage          MetricType = "cpu_usage"
	MetricTypeMemoryUsage       MetricType = "memory_usage"
	MetricTypeHTTPRequestCount  MetricType = "http_request_count"
	MetricTypeActiveConnections MetricType = "active_connections"
	MetricTypeInstanceCount     MetricType = "instance_count"
	MetricTypeHTTPLatency       MetricType = "http_latency"
	MetricTypeCPULimit          MetricType = "cpu_limit"
	MetricTypeCPUTarget         MetricType = "cpu_target"
	MetricTypeMemoryLimit       MetricType = "memory_limit"
	MetricTypeMemoryTarget      MetricType = "memory_target"
	MetricTypeBandwidthUsage    MetricType = "bandwidth_usage"
)

type MetricsRequest struct {
	ResourceID                  string
	MetricTypes                 []MetricType
	StartTime                   *client.StartTimeParam
	EndTime                     *client.EndTimeParam
	Resolution                  *float32
	CpuUsageAggregationMethod   *metricstypes.ApplicationMetricAggregationMethod
	AggregateHttpRequestCountBy *metricstypes.HttpAggregateBy
	HttpLatencyQuantile         *metricstypes.Quantile
	HttpPath                    *metricstypes.PathQueryParam
	HttpHost                    *metricstypes.HostQueryParam
}

type MetricData struct {
	Type MetricType                        `json:"type"`
	Data metricstypes.TimeSeriesCollection `json:"data"`
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
	_, err := session.FromContext(ctx).GetWorkspace(ctx)
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
		data, err := r.fetchMetric(ctx, req.ResourceID, metricType, req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s metrics: %w", metricType, err)
		}
		response.Metrics = append(response.Metrics, data)
	}

	return response, nil
}

func (r *Repo) fetchMetric(ctx context.Context, resourceId string, metricType MetricType, req MetricsRequest) (MetricData, error) {
	var data metricstypes.TimeSeriesCollection
	var err error

	switch metricType {
	case MetricTypeCPUUsage:
		data, err = r.getCpuUsage(ctx, resourceId, req)
	case MetricTypeMemoryUsage:
		data, err = r.getMemoryUsage(ctx, resourceId, req)
	case MetricTypeHTTPRequestCount:
		data, err = r.getHttpRequestCount(ctx, resourceId, req)
	case MetricTypeActiveConnections:
		data, err = r.getActiveConnectionsMetrics(ctx, resourceId, req)
	case MetricTypeInstanceCount:
		data, err = r.getInstanceCountMetrics(ctx, resourceId, req)
	case MetricTypeHTTPLatency:
		data, err = r.getHttpLatency(ctx, resourceId, req)
	case MetricTypeCPULimit:
		data, err = r.getCpuLimit(ctx, resourceId, req)
	case MetricTypeCPUTarget:
		data, err = r.getCpuTarget(ctx, resourceId, req)
	case MetricTypeMemoryLimit:
		data, err = r.getMemoryLimit(ctx, resourceId, req)
	case MetricTypeMemoryTarget:
		data, err = r.getMemoryTarget(ctx, resourceId, req)
	case MetricTypeBandwidthUsage:
		data, err = r.getBandwidthUsage(ctx, resourceId, req)
	default:
		return MetricData{}, fmt.Errorf("unsupported metric type: %s", metricType)
	}

	if err != nil {
		return MetricData{}, err
	}

	return MetricData{
		Type: metricType,
		Data: data,
	}, nil
}

func (r *Repo) getCpuUsage(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetCpuParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	if req.CpuUsageAggregationMethod != nil {
		params.AggregationMethod = req.CpuUsageAggregationMethod
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetCpuWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from CPU metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getMemoryUsage(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetMemoryParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetMemoryWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from memory metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getHttpRequestCount(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {

	params := &client.GetHttpRequestsParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	if req.AggregateHttpRequestCountBy != nil {
		params.AggregateBy = req.AggregateHttpRequestCountBy
	}

	if req.HttpHost != nil {
		params.Host = req.HttpHost
	}

	if req.HttpPath != nil {
		params.Path = req.HttpPath
	}

	resp, err := r.client.GetHttpRequestsWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("HTTP metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, nil
	}

	return *resp.JSON200, nil
}

func (r *Repo) getActiveConnectionsMetrics(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetActiveConnectionsParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Use DatastoreResourceQueryParam for active connections
	datastoreResource := metricstypes.DatastoreResourceQueryParam(resourceId)
	params.Resource = &datastoreResource

	resp, err := r.client.GetActiveConnectionsWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get connections metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("connections metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, nil
	}

	return *resp.JSON200, nil
}

func (r *Repo) getInstanceCountMetrics(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
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
		return nil, fmt.Errorf("failed to get instance count metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("instance count metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, nil
	}

	return *resp.JSON200, nil
}

func (r *Repo) getHttpLatency(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetHttpLatencyParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	// Set quantile parameter - default to 0.95 if not specified
	if req.HttpLatencyQuantile != nil {
		params.Quantile = req.HttpLatencyQuantile
	} else {
		defaultQuantile := metricstypes.Quantile(0.95)
		params.Quantile = &defaultQuantile
	}

	if req.HttpHost != nil {
		params.Host = req.HttpHost
	}

	if req.HttpPath != nil {
		params.Path = req.HttpPath
	}

	resp, err := r.client.GetHttpLatencyWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get response time metrics: %w", err)
	}

	if resp.StatusCode() == 400 {
		// This API only works for paid tier accounts; it will return a 400 for Hobby tier.
		return metricstypes.TimeSeriesCollection{}, nil
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("response time metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, nil
	}

	return *resp.JSON200, nil
}

func (r *Repo) getCpuLimit(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetCpuLimitParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetCpuLimitWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU limit metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from CPU limit metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getCpuTarget(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetCpuTargetParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetCpuTargetWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU target metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from CPU target metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getMemoryLimit(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetMemoryLimitParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetMemoryLimitWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory limit metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from memory limit metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getMemoryTarget(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetMemoryTargetParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if req.Resolution != nil {
		resolutionParam := metricstypes.ResolutionParam(*req.Resolution)
		params.ResolutionSeconds = &resolutionParam
	}

	// Set resource parameter - use the generic Resource field for all types
	resource := metricstypes.ResourceQueryParam(resourceId)
	params.Resource = &resource

	resp, err := r.client.GetMemoryTargetWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory target metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("empty response from memory target metrics API")
	}

	return *resp.JSON200, nil
}

func (r *Repo) getBandwidthUsage(ctx context.Context, resourceId string, req MetricsRequest) (metricstypes.TimeSeriesCollection, error) {
	params := &client.GetBandwidthParams{
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	// Set resource parameter - use ServiceResourceQueryParam for bandwidth
	serviceResource := metricstypes.ServiceResourceQueryParam(resourceId)
	params.Resource = &serviceResource

	resp, err := r.client.GetBandwidthWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get bandwidth usage metrics: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("bandwidth usage metrics API returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return metricstypes.TimeSeriesCollection{}, nil
	}

	return *resp.JSON200, nil
}
