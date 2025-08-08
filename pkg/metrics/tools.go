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
				"Supports CPU, memory, HTTP request, connection, instance count, HTTP error, and response time metrics for debugging, capacity planning, and performance optimization. "+
				"Returns time-series data with timestamps and values for the specified time range. "+
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
					"CPU and memory are available for all resources. "+
					"HTTP, instance count, HTTP error, and response time metrics are only available for services. "+
					"Connection metrics are only available for databases and key-value stores."),
				mcp.Items(map[string]interface{}{
					"type": "string",
					"enum": []string{string(MetricTypeCPU), string(MetricTypeMemory), string(MetricTypeHTTP), string(MetricTypeConnections), string(MetricTypeInstanceCount), string(MetricTypeHTTPErrors), string(MetricTypeResponseTime)},
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
					"API defaults to 60 seconds if not provided."),
				mcp.Min(30),
			),
			mcp.WithString("aggregationMethod",
				mcp.Description("Method for aggregating metric values over time intervals"),
				mcp.Enum(string(metricstypes.AVG), string(metricstypes.MAX), string(metricstypes.MIN)),
				mcp.DefaultString(string(metricstypes.AVG)),
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
				case MetricTypeCPU, MetricTypeMemory, MetricTypeHTTP, MetricTypeConnections, MetricTypeInstanceCount, MetricTypeHTTPErrors, MetricTypeResponseTime:
					metricTypes = append(metricTypes, metricType)
				default:
					return mcp.NewToolResultError(fmt.Sprintf("invalid metric type: %s. Must be one of: cpu, memory, http, connections, instancecount, httperrors, responsetime", mtStr)), nil
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

			if aggregationMethod, ok, err := validate.OptionalToolParam[string](request, "aggregationMethod"); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("aggregationMethod parameter error: %s", err.Error())), nil
			} else if ok {
				method := metricstypes.ApplicationMetricAggregationMethod(aggregationMethod)
				if method != metricstypes.AVG && method != metricstypes.MAX && method != metricstypes.MIN {
					return mcp.NewToolResultError(fmt.Sprintf("invalid aggregationMethod: %s. Must be one of: AVG, MAX, MIN", aggregationMethod)), nil
				}
				metricsRequest.AggregationMethod = &method
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
