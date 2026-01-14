# Render MCP Server

## Overview

The Render MCP Server is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction)
server that allows you to interact with your Render resources via LLMs.

## Getting Started

Get started with the MCP server by following the official docs: https://render.com/docs/mcp-server

## Use Cases

- Creating and managing web services, static sites, cron jobs, and databases on Render
- Monitoring application logs and deployment status to help troubleshoot issues
- Monitoring service performance metrics for debugging, capacity planning, and optimization
- Querying your Postgres databases directly inside an LLM

## Feedback

Please leave feedback via
[filing a GitHub issue](https://github.com/render-oss/render-mcp-server/issues) if you have any
feature requests, bug reports, suggestions, comments, or concerns.

## Tools

### Workspaces

- **list_workspaces** - List the workspaces that you have access to

  - No parameters required

- **select_workspace** - Select a workspace to use

  - `ownerID`: The ID of the workspace to use (string, required)

- **get_selected_workspace** - Get the currently selected workspace
  - No parameters required

### Services

- **list_services** - List all services in your Render account

  - `includePreviews`: Whether to include preview services, defaults to false (boolean, optional)

- **get_service** - Get details about a specific service

  - `serviceId`: The ID of the service to retrieve (string, required)

- **create_web_service** - Create a new web service in your Render account

  - `name`: A unique name for your service (string, required)
  - `runtime`: Runtime environment for your service (string, required). Accepted values:
    - `node`
    - `python`
    - `go`
    - `rust`
    - `ruby`
    - `elixir`
    - `docker`
  - `buildCommand`: Command used to build your service (string, required)
  - `startCommand`: Command used to start your service (string, required)
  - `repo`: Repository containing source code (string, optional)
  - `branch`: Repository branch to deploy (string, optional)
  - `plan`: Plan for your service (string, optional). Accepted values:
    - `starter`
    - `standard`
    - `pro`
    - `pro_max`
    - `pro_plus`
    - `pro_ultra`
  - `autoDeploy`: Whether to automatically deploy the service (string, optional). Defaults to `yes`. Accepted values:
    - `yes`: Enable automatic deployments
    - `no`: Disable automatic deployments
  - `region`: Geographic region for deployment (string, optional). Defaults to `oregon`. Accepted values:
    - `oregon`
    - `frankfurt`
    - `singapore`
    - `ohio`
    - `virginia`
  - `envVars`: Environment variables array (array, optional)

- **create_static_site** - Create a new static site in your Render account

  - `name`: A unique name for your service (string, required)
  - `buildCommand`: Command to build your app (string, required)
  - `repo`: Repository containing source code (string, optional)
  - `branch`: Repository branch to deploy (string, optional)
  - `autoDeploy`: Whether to automatically deploy the service (string, optional). Defaults to `yes`. Accepted values:
    - `yes`: Enable automatic deployments
    - `no`: Disable automatic deployments
  - `publishPath`: Directory containing built assets (string, optional)
  - `envVars`: Environment variables array (array, optional)

- **create_cron_job** - Create a new cron job in your Render account

  - `name`: A unique name for your cron job (string, required)
  - `schedule`: Cron schedule expression (string, required). Uses standard cron syntax with 5 fields: minute (0-59), hour (0-23), day of month (1-31), month (1-12), day of week (0-6, Sunday=0). Examples:
    - `0 0 * * *`: Daily at midnight
    - `*/15 * * * *`: Every 15 minutes
    - `0 9 * * 1-5`: Weekdays at 9am
    - `0 0 1 * *`: First day of each month at midnight
  - `runtime`: Runtime environment for your cron job (string, required). Accepted values:
    - `node`
    - `python`
    - `go`
    - `rust`
    - `ruby`
    - `elixir`
    - `docker`
  - `buildCommand`: Command used to build your cron job (string, required)
  - `startCommand`: Command that runs when your cron job executes (string, required)
  - `repo`: Repository containing source code (string, optional)
  - `branch`: Repository branch to deploy (string, optional)
  - `plan`: Plan for your cron job (string, optional). Accepted values:
    - `starter`
    - `standard`
    - `pro`
    - `pro_max`
    - `pro_plus`
    - `pro_ultra`
  - `autoDeploy`: Whether to automatically deploy the cron job (string, optional). Defaults to `yes`. Accepted values:
    - `yes`: Enable automatic deployments
    - `no`: Disable automatic deployments
  - `region`: Geographic region for deployment (string, optional). Defaults to `oregon`. Accepted values:
    - `oregon`
    - `frankfurt`
    - `singapore`
    - `ohio`
    - `virginia`
  - `envVars`: Environment variables array (array, optional)

- **update_environment_variables** - Update all environment variables for a service
  - `serviceId`: The ID of the service to update (string, required)
  - `envVars`: Complete list of environment variables (array, required)

### Deployments

- **list_deploys** - List deployment history for a service

  - `serviceId`: The ID of the service to get deployments for (string, required)

- **get_deploy** - Get details about a specific deployment
  - `serviceId`: The ID of the service (string, required)
  - `deployId`: The ID of the deployment (string, required)

### Logs

- **list_logs** - List logs matching the provided filters

  - `resource`: Filter logs by their resource (array of strings, required)
  - `level`: Filter logs by their severity level (array of strings, optional)
  - `type`: Filter logs by their type (array of strings, optional)
  - `instance`: Filter logs by the instance they were emitted from (array of strings, optional)
  - `host`: Filter request logs by their host (array of strings, optional)
  - `statusCode`: Filter request logs by their status code (array of strings, optional)
  - `method`: Filter request logs by their requests method (array of strings, optional)
  - `path`: Filter request logs by their path (array of strings, optional)
  - `text`: Filter by the text of the logs (array of strings, optional)
  - `startTime`: Start time for log query (RFC3339 format) (string, optional)
  - `endTime`: End time for log query (RFC3339 format) (string, optional)
  - `direction`: The direction to query logs for (string, optional)
  - `limit`: Maximum number of logs to return (number, optional)

- **list_log_label_values** - List all values for a given log label in the logs matching the provided filters
  - `label`: The label to list values for (string, required)
  - `resource`: Filter by resource (array of strings, required)
  - `level`: Filter logs by their severity level (array of strings, optional)
  - `type`: Filter logs by their type (array of strings, optional)
  - `instance`: Filter logs by the instance they were emitted from (array of strings, optional)
  - `host`: Filter request logs by their host (array of strings, optional)
  - `statusCode`: Filter request logs by their status code (array of strings, optional)
  - `method`: Filter request logs by their requests method (array of strings, optional)
  - `path`: Filter request logs by their path (array of strings, optional)
  - `text`: Filter by the text of the logs (array of strings, optional)
  - `startTime`: Start time for log query (RFC3339 format) (string, optional)
  - `endTime`: End time for log query (RFC3339 format) (string, optional)
  - `direction`: The direction to query logs for (string, optional)

### Metrics

- **get_metrics** - Get performance metrics for any Render resource (services, Postgres databases, key-value stores). Metrics may be empty if the metric is not valid for the given resource
  - `resourceId`: The ID of the resource to get metrics for (service ID, Postgres ID, or key-value store ID) (string, required)
  - `metricTypes`: Which metrics to fetch (array of strings, required). Accepted values:
    - `cpu_usage`: CPU usage metrics (available for all resources)
    - `cpu_limit`: CPU resource constraints (available for all resources)
    - `cpu_target`: CPU autoscaling thresholds (available for all resources)
    - `memory_usage`: Memory usage metrics (available for all resources)
    - `memory_limit`: Memory resource constraints (available for all resources)
    - `memory_target`: Memory autoscaling thresholds (available for all resources)
    - `instance_count`: Instance count metrics (available for all resources)
    - `http_request_count`: HTTP request count metrics (services only)
    - `http_latency`: HTTP response time metrics (services only)
    - `bandwidth_usage`: Bandwidth usage metrics (services only)
    - `active_connections`: Active connection metrics (databases and key-value stores only)
  - `startTime`: Start time for metrics query in RFC3339 format (e.g., '2024-01-01T12:00:00Z'), defaults to 1 hour ago. The start time must be within the last 30 days (string, optional)
  - `endTime`: End time for metrics query in RFC3339 format (e.g., '2024-01-01T13:00:00Z'), defaults to the current time. The end time must be within the last 30 days (string, optional)
  - `resolution`: Time resolution for data points in seconds. Lower values provide more granular data. Higher values provide more aggregated data points. API defaults to 60 seconds if not provided, minimum 30 seconds (number, optional)
  - `cpuUsageAggregationMethod`: Method for aggregating CPU usage metric values over time intervals (string, optional). Defaults to `AVG`. Accepted values:
    - `AVG`: Average CPU usage over time intervals
    - `MAX`: Maximum CPU usage over time intervals
    - `MIN`: Minimum CPU usage over time intervals
  - `aggregateHttpRequestCountsBy`: Field to aggregate HTTP request count metrics by (string, optional). When not specified, returns total request counts. Accepted values:
    - `host`: Aggregate by request host
    - `statusCode`: Aggregate by HTTP status code
  - `httpLatencyQuantile`: The quantile/percentile of HTTP latency to fetch. Only supported for http_latency metric. Common values: 0.5 (median), 0.95 (95th percentile), 0.99 (99th percentile). Defaults to 0.95 if not specified (number, optional, min: 0.0, max: 1.0)
  - `httpHost`: Filter HTTP metrics to specific request hosts. Supported for http_request_count and http_latency metrics. Example: 'api.example.com' or 'myapp.render.com'. When not specified, includes all hosts (string, optional)
  - `httpPath`: Filter HTTP metrics to specific request paths. Supported for http_request_count and http_latency metrics. Example: '/api/users' or '/health'. When not specified, includes all paths (string, optional)

### Postgres Databases

- **query_render_postgres** - Run a read-only SQL query against a Render-hosted Postgres database

  - `postgresId`: The ID of the Postgres instance to query (string, required)
  - `sql`: The SQL query to run (string, required)

- **list_postgres_instances** - List all PostgreSQL databases in your Render account

  - No parameters required

- **get_postgres** - Get details about a specific PostgreSQL database

  - `postgresId`: The ID of the PostgreSQL database to retrieve (string, required)

- **create_postgres** - Create a new PostgreSQL database
  - `name`: Name of the PostgreSQL database (string, required)
  - `plan`: Pricing plan for the database (string, required). Accepted values:
    - `free`
    - `basic_256mb`
    - `basic_1gb`
    - `basic_4gb`
    - `pro_4gb`
    - `pro_8gb`
    - `pro_16gb`
    - `pro_32gb`
    - `pro_64gb`
    - `pro_128gb`
    - `pro_192gb`
    - `pro_256gb`
    - `pro_384gb`
    - `pro_512gb`
    - `accelerated_16gb`
    - `accelerated_32gb`
    - `accelerated_64gb`
    - `accelerated_128gb`
    - `accelerated_256gb`
    - `accelerated_384gb`
    - `accelerated_512gb`
    - `accelerated_768gb`
    - `accelerated_1024gb`
  - `region`: Region for deployment (string, optional). Accepted values:
    - `oregon`
    - `frankfurt`
    - `singapore`
    - `ohio`
    - `virginia`
  - `version`: PostgreSQL version to use (e.g., 14, 15) (number, optional)
  - `diskSizeGb`: Database capacity in GB (number, optional)

### Key Value instances

- **list_key_value** - List all Key Value instances in your Render account

  - No parameters required

- **get_key_value** - Get details about a specific Key Value instance

  - `keyValueId`: The ID of the Key Value instance to retrieve (string, required)

- **create_key_value** - Create a new Key Value instance
  - `name`: Name of the Key Value instance (string, required)
  - `plan`: Pricing plan for the Key Value instance (string, required). Accepted values:
    - `free`
    - `starter`
    - `standard`
    - `pro`
    - `pro_plus`
  - `region`: Region for deployment (string, optional). Accepted values:
    - `oregon`
    - `frankfurt`
    - `singapore`
    - `ohio`
    - `virginia`
  - `maxmemoryPolicy`: Eviction policy for the Key Value store (string, optional). Accepted values:
    - `noeviction`: No eviction policy (may cause memory errors)
    - `allkeys_lfu`: Evict least frequently used keys from all keys
    - `allkeys_lru`: Evict least recently used keys from all keys
    - `allkeys_random`: Evict random keys from all keys
    - `volatile_lfu`: Evict least frequently used keys from keys with expiration
    - `volatile_lru`: Evict least recently used keys from keys with expiration
    - `volatile_random`: Evict random keys from keys with expiration
    - `volatile_ttl`: Evict keys with shortest time to live from keys with expiration