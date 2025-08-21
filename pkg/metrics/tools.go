package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	metricstypes "github.com/render-oss/render-mcp-server/pkg/client/metrics"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	metricsRepo := NewRepo(c)

	return []server.ServerTool{
		getMetrics(metricsRepo),
	}
}

func getMetrics(metricsRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_metrics",
			mcp.WithDescription("Get performance metrics for any Render resource (services, Postgres databases, key-value stores). "+
				"Supports CPU usage/limits/targets, memory usage/limits/targets, service instance counts, HTTP request counts and response time metrics, database active connection counts for debugging, capacity planning, and performance optimization. "+
				"Returns time-series data with timestamps and values for the specified time range. "+
				"HTTP metrics support filtering by host and path for more granular analysis. "+
				"Limits and targets help understand resource constraints and autoscaling thresholds. "+
				"Metrics may be empty if the metric is not valid for the given resource."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:         "Get resource metrics",
				ReadOnlyHint:  pointers.From(true),
				OpenWorldHint: pointers.From(true),
			}),
			mcp.WithString("resourceId",
				mcp.Required(),
				mcp.Description("The ID of the resource to get metrics for (service ID, Postgres ID, or key-value store ID)"),
			),
			mcp.WithArray("metricTypes",
				mcp.Required(),
				mcp.Description("Which metrics to fetch. "+
					"CPU usage/limits/targets, memory usage/limits/targets, and instance count metrics are available for all resources. "+
					"HTTP request counts and response time metrics are only available for services. "+
					"Active connection metrics are only available for databases and key-value stores. "+
					"Limits show resource constraints, targets show autoscaling thresholds."),
				mcp.Items(map[string]interface{}{
					"type": "string",
					"enum": []string{
						string(MetricTypeCPUUsage), string(MetricTypeMemoryUsage),
						string(MetricTypeHTTPRequestCount), string(MetricTypeActiveConnections),
						string(MetricTypeInstanceCount), string(MetricTypeHTTPLatency),
						string(MetricTypeCPULimit), string(MetricTypeCPUTarget),
						string(MetricTypeMemoryLimit), string(MetricTypeMemoryTarget),
					},
				}),
			),
			mcp.WithString("startTime",
				mcp.Description("Start time for metrics query in RFC3339 format (e.g., '2024-01-01T12:00:00Z'). "+
					"Defaults to 1 hour ago. "+
					"The start time must be within the last 30 days."),
			),
			mcp.WithString("endTime",
				mcp.Description("End time for metrics query in RFC3339 format (e.g., '2024-01-01T13:00:00Z'). "+
					"Defaults to the current time. "+
					"The end time must be within the last 30 days."),
			),
			mcp.WithNumber("resolution",
				mcp.Description("Time resolution for data points in seconds. "+
					"Lower values provide more granular data. "+
					"Higher values provide more aggregated data points. "+
					"API defaults to 60 seconds if not provided. "+
					"There is a limit to the number of data points that can be returned, after which the metrics endpoint will return a 500. "+
					"If you are getting a 500, try reducing granularity (increasing the value of resolution)."),
				mcp.Min(30),
			),
			mcp.WithString("cpuUsageAggregationMethod",
				mcp.Description("Method for aggregating metric values over time intervals. "+
					"Only supported for CPU usage metrics. "+
					"Options: AVG, MAX, MIN. Defaults to AVG."),
				mcp.Enum(string(metricstypes.AVG), string(metricstypes.MAX), string(metricstypes.MIN)),
				mcp.DefaultString(string(metricstypes.AVG)),
			),
			mcp.WithString("aggregateHttpRequestCountsBy",
				mcp.Description("Field to aggregate HTTP request metrics by. "+
					"Only supported for http_request_count metric. "+
					"Options: host (aggregate by request host), statusCode (aggregate by HTTP status code). "+
					"When not specified, returns total request counts."),
				mcp.Enum(string(metricstypes.HttpAggregateByHost), string(metricstypes.HttpAggregateByStatusCode)),
			),
			mcp.WithNumber("httpLatencyQuantile",
				mcp.Description("The quantile/percentile of HTTP latency to fetch. "+
					"Only supported for http_latency metric. "+
					"Common values: 0.5 (median), 0.95 (95th percentile), 0.99 (99th percentile). "+
					"Defaults to 0.95 if not specified."),
				mcp.Min(0.0),
				mcp.Max(1.0),
				mcp.DefaultNumber(0.95),
			),
			mcp.WithString("httpHost",
				mcp.Description("Filter HTTP metrics to specific request hosts. "+
					"Supported for http_request_count and http_latency metrics. "+
					"Example: 'api.example.com' or 'myapp.render.com'. "+
					"When not specified, includes all hosts."),
			),
			mcp.WithString("httpPath",
				mcp.Description("Filter HTTP metrics to specific request paths. "+
					"Supported for http_request_count and http_latency metrics. "+
					"Example: '/api/users' or '/health'. "+
					"When not specified, includes all paths."),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceId, err := validate.RequiredToolParam[string](request, "resourceId")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("resourceId parameter error: %s", err.Error())), nil
			}

			metricTypesRaw, err := validate.RequiredToolArrayParam[interface{}](request, "metricTypes")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("metricTypes parameter error: %s", err.Error())), nil
			}

			var metricTypes []MetricType
			for _, mt := range metricTypesRaw {
				mtStr, ok := mt.(string)
				if !ok {
					return mcp.NewToolResultError("metricTypes must be an array of strings"), nil
				}

				metricType := MetricType(mtStr)
				switch metricType {
				case MetricTypeCPUUsage, MetricTypeMemoryUsage, MetricTypeHTTPRequestCount, MetricTypeActiveConnections, MetricTypeInstanceCount, MetricTypeHTTPLatency, MetricTypeCPULimit, MetricTypeCPUTarget, MetricTypeMemoryLimit, MetricTypeMemoryTarget:
					metricTypes = append(metricTypes, metricType)
				default:
					return mcp.NewToolResultError(fmt.Sprintf("invalid metric type: %s. Must be one of: cpu_usage, memory_usage, http_request_count, active_connections, instance_count, http_latency, cpu_limit, cpu_target, memory_limit, memory_target", mtStr)), nil
				}
			}

			if len(metricTypes) == 0 {
				return mcp.NewToolResultError("at least one metric type must be specified"), nil
			}

			metricsRequest := MetricsRequest{
				ResourceID:  resourceId,
				MetricTypes: metricTypes,
			}

			// Parse optional time parameters
			if startTimeStr, ok, err := validate.OptionalToolParam[string](request, "startTime"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("startTime parameter error: %s", err.Error())), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, startTimeStr)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("invalid startTime format, expected RFC3339: %s", err.Error())), nil
				}
				startTimeParam := client.StartTimeParam(parsedTime)
				metricsRequest.StartTime = &startTimeParam
			}

			if endTimeStr, ok, err := validate.OptionalToolParam[string](request, "endTime"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("endTime parameter error: %s", err.Error())), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, endTimeStr)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("invalid endTime format, expected RFC3339: %s", err.Error())), nil
				}
				endTimeParam := client.EndTimeParam(parsedTime)
				metricsRequest.EndTime = &endTimeParam
			}

			if resolution, ok, err := validate.OptionalToolParam[float64](request, "resolution"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("resolution parameter error: %s", err.Error())), nil
			} else if ok {
				resolutionFloat32 := float32(resolution)
				metricsRequest.Resolution = &resolutionFloat32
			}

			if cpuAgg, ok, err := validate.OptionalToolParam[string](request, "cpuUsageAggregationMethod"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("cpuUsageAggregationMethod parameter error: %s", err.Error())), nil
			} else if ok {
				method := metricstypes.ApplicationMetricAggregationMethod(cpuAgg)
				if method != metricstypes.AVG && method != metricstypes.MAX && method != metricstypes.MIN {
					return mcp.NewToolResultError(fmt.Sprintf("invalid cpuUsageAggregationMethod: %s. Must be one of: AVG, MAX, MIN", cpuAgg)), nil
				}
				metricsRequest.CpuUsageAggregationMethod = &method
			}

			if aggregateBy, ok, err := validate.OptionalToolParam[string](request, "aggregateHttpRequestCountsBy"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("aggregateHttpRequestCountsBy parameter error: %s", err.Error())), nil
			} else if ok {
				agg := metricstypes.HttpAggregateBy(aggregateBy)
				if agg != metricstypes.HttpAggregateByHost && agg != metricstypes.HttpAggregateByStatusCode {
					return mcp.NewToolResultError(fmt.Sprintf("invalid aggregateBy: %s. Must be one of: host, statusCode", aggregateBy)), nil
				}
				metricsRequest.AggregateHttpRequestCountBy = &agg
			}

			if quantile, ok, err := validate.OptionalToolParam[float64](request, "httpLatencyQuantile"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("httpLatencyQuantile parameter error: %s", err.Error())), nil
			} else if ok {
				if quantile < 0.0 || quantile > 1.0 {
					return mcp.NewToolResultError(fmt.Sprintf("invalid httpLatencyQuantile: %f. Must be between 0.0 and 1.0", quantile)), nil
				}
				quantileFloat32 := metricstypes.Quantile(quantile)
				metricsRequest.HttpLatencyQuantile = &quantileFloat32
			}

			if host, ok, err := validate.OptionalToolParam[string](request, "httpHost"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("httpHost parameter error: %s", err.Error())), nil
			} else if ok {
				hostParam := metricstypes.HostQueryParam(host)
				metricsRequest.HttpHost = &hostParam
			}

			if path, ok, err := validate.OptionalToolParam[string](request, "httpPath"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("httpPath parameter error: %s", err.Error())), nil
			} else if ok {
				pathParam := metricstypes.PathQueryParam(path)
				metricsRequest.HttpPath = &pathParam
			}

			response, err := metricsRepo.GetMetrics(ctx, metricsRequest)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to get metrics: %s", err.Error())), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %s", err.Error())), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}
