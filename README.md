# Render MCP Server

## Overview
The Render MCP Server is an early access [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction)
server that allows you to interact with your Render resources via LLMs.

## Use Cases

- Creating and managing web services, static sites, and databases on Render
- Monitoring application logs and deployment status to help troubleshoot issues
- Monitoring service performance metrics for debugging, capacity planning, and optimization
- Querying your Postgres databases directly inside an LLM

## Feedback

The official Render MCP server is currently in early access. Please leave feedback via 
[filing a GitHub issue](https://github.com/render-oss/render-mcp-server/issues) if you have any 
feature requests, bug reports, suggestions, comments, or concerns.

## Getting Started

This guide will help you set up the Render MCP Server. To use the server, you will need a desktop application that can act as an MCP client (e.g., Claude Desktop, Cursor IDE, VS Code). All installation methods require a Render API Key, and you will configure your chosen MCP client with the server details.

### 1. Obtain a Render API Key
You must create a Render API key from your [Render Dashboard → Account Settings → API Keys](https://dashboard.render.com/settings#api-keys).

> [!IMPORTANT]
> Render API keys are currently broadly scoped, giving your AI tools the same permissions that you would have access to. This MCP server avoids destructive operations, but please make sure you're comfortable granting your AI tools these permissions. 

### 2. Choose an Installation Method

Select one of the following methods to install and run the Render MCP Server.

#### Method A: Using Docker (Recommended)
This is the simplest way to get started if you have [Docker](https://www.docker.com) installed and running.

**Steps:**
1. Ensure Docker is installed and operational on your system.
2. Configure your MCP client with the following settings (or by using the one-click install buttons below), replacing `<YOUR_API_KEY>` with the API key you obtained in Step 1:
   ```json
   {
     "mcpServers": {
       "render": {
         "command": "docker",
         "args": [
           "run",
           "-i",
           "--rm",
           "-e",
           "RENDER_API_KEY",
           "-v",
           "render-mcp-server-config:/config",
           "ghcr.io/render-oss/render-mcp-server"
         ],
         "env": {
           "RENDER_API_KEY": "<YOUR_API_KEY>"
         }
       }
     }
   }
   ```

[![Add to Cursor](https://cursor.com/deeplink/mcp-install-light.svg)](https://cursor.com/install-mcp?name=render&config=eyJjb21tYW5kIjoiZG9ja2VyIHJ1biAtaSAtLXJtIC1lIFJFTkRFUl9BUElfS0VZIC12IHJlbmRlci1tY3Atc2VydmVyLWNvbmZpZzovY29uZmlnIGdoY3IuaW8vcmVuZGVyLW9zcy9yZW5kZXItbWNwLXNlcnZlciIsImVudiI6eyJSRU5ERVJfQVBJX0tFWSI6IllPVVJfQVBJX0tFWSJ9fQ%3D%3D)

#### Method B: Using the install script (Linux/MacOS only)
1. Run the following command:
```shell
curl -fsSL https://raw.githubusercontent.com/render-oss/render-mcp-server/refs/heads/main/bin/install.sh | sh
```
2. Note the full path where the install script saved the downloaded executable. It should have a directory where it was installed e.g., `✨ Successfully installed Render MCP Server to /Users/example/.local/bin/render-mcp-server`
2. Configure your MCP client with the following settings (or by using the one-click install buttons below). Replace `/path/to/render-mcp-server` with the actual path to the executable and `<YOUR_API_KEY>` with your API key:
   ```json
   {
     "mcpServers": {
       "render": {
         "command": "/path/to/render-mcp-server",
         "env": {
           "RENDER_API_KEY": "<YOUR_API_KEY>"
         }
       }
     }
   }
   ```

[![Add to Cursor](https://cursor.com/deeplink/mcp-install-light.svg)](https://cursor.com/install-mcp?name=render&config=eyJjb21tYW5kIjoiL3BhdGgvdG8vcmVuZGVyLW1jcC1zZXJ2ZXIiLCJlbnYiOnsiUkVOREVSX0FQSV9LRVkiOiI8WU9VUl9BUElfS0VZPiJ9fQ%3D%3D)

#### Method C: Direct Download
Use this method if you prefer not to use Docker and a pre-compiled binary is available for your system.

**Steps:**
1. Open the MCP server's [GitHub releases page](https://github.com/render-oss/render-mcp-server/releases/).
2. Download the executable that corresponds to your system's architecture.
3. Note the full path to where you saved the downloaded executable.
4. Configure your MCP client with the same settings as Method B.
   > **macOS Users**: If you run the binary directly on macOS, you may need to grant an exception for it to run. See the [Limitations](#limitations) section for more details and a link to Apple's support page.

#### Method D: Build from Source
Choose this method if no pre-compiled binary suits your system, you want to build from the latest code, or you are a developer modifying the server. You will need [Go (Golang)](https://go.dev/doc/install) installed.

**Steps:**
1. Ensure Go is installed on your system.
2. Clone the repository and build the executable:
   ```shell
   git clone https://github.com/render-oss/render-mcp-server.git
   cd render-mcp-server
   go build
   ```
   This will create a `render-mcp-server` executable in the `render-mcp-server` directory.
3. Note the full path to this newly built executable (e.g., `./render-mcp-server` if you are in the directory, or the full absolute path).
4. Configure your MCP client with the same settings as Method B.

## Limitations

> [!NOTE]
> The MCP server is currently in early access, and there are several limitations. If you have specific
feedback or would like to report a bug or feature request, please [create a GitHub Issue](https://github.com/render-oss/render-mcp-server/issues). 

1. **macOS Users**: If you download and run the binary directly on macOS, you may need to grant an exception to run it as it's from an "unknown developer". You can find instructions on how to do this [here](https://support.apple.com/guide/mac-help/open-a-mac-app-from-an-unknown-developer-mh40616/mac). This issue might not present a pop-up if the binary is launched from within another application like Claude or Cursor. This is not an issue if you are launching the MCP server via Docker.

2. The Render MCP server currently only allows you to create the following service types: web services and static sites. Other service types, including cronjobs, private services, and background workers are not currently supported.

3. The Render MCP server does not currently support all configuration options when creating services. For example, you cannot create image-backed services or set up IP address restrictions. If there are options that you would like to see supported and aren't today, please [let us know](https://github.com/render-oss/render-mcp-server/issues).

4. You cannot perform service mutations (updates) or deletions using this MCP server. Please use the Render dashboard or the REST API for these operations.

5. Manual triggering of deployments is not currently supported via this MCP server.

6. The Render MCP server does not allow creating free services.

7. The Render MCP server attempts to minimize exposing sensitive information (like connection strings) to the MCP host's context. However, we make no guarantees about this behavior, and users should remain vigilant when discussing sensitive data.

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
  - `runtime`: Runtime environment for your service. Accepted values: 'node', 'python', 'go', 'rust', 'ruby', 'elixir', 'docker' (string, required)
  - `buildCommand`: Command used to build your service (string, required)
  - `startCommand`: Command used to start your service (string, required)
  - `repo`: Repository containing source code (string, optional)
  - `branch`: Repository branch to deploy (string, optional)
  - `plan`: Plan for your service. Accepted values: 'starter', 'standard', 'pro', 'pro_max', 'pro_plus', 'pro_ultra' (string, optional)
  - `autoDeploy`: Whether to automatically deploy the service. Accepted values: 'yes', 'no'. Defaults to 'yes' (string, optional)
  - `region`: Geographic region for deployment. Accepted values: 'oregon', 'frankfurt', 'singapore', 'ohio', 'virginia'. Defaults to 'oregon' (string, optional)
  - `envVars`: Environment variables array (array, optional)

- **create_static_site** - Create a new static site in your Render account
  - `name`: A unique name for your service (string, required)
  - `buildCommand`: Command to build your app (string, required)
  - `repo`: Repository containing source code (string, optional)
  - `branch`: Repository branch to deploy (string, optional)
  - `autoDeploy`: Whether to automatically deploy the service. Accepted values: 'yes', 'no'. Defaults to 'yes' (string, optional)
  - `publishPath`: Directory containing built assets (string, optional)
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
  - `metricTypes`: Which metrics to fetch (array of strings, required). Accepted values: 'cpu_usage', 'cpu_limit', 'cpu_target', 'memory_usage', 'memory_limit', 'memory_target', 'http_request_count', 'active_connections', 'instance_count', 'http_latency', 'bandwidth_usage'. CPU usage/limits/targets, memory usage/limits/targets, and instance count metrics are available for all resources. HTTP request counts and response time metrics, and bandwidth usage metrics are only available for services. Active connection metrics are only available for databases and key-value stores. Limits show resource constraints, targets show autoscaling thresholds
  - `startTime`: Start time for metrics query in RFC3339 format (e.g., '2024-01-01T12:00:00Z'), defaults to 1 hour ago. The start time must be within the last 30 days (string, optional)
  - `endTime`: End time for metrics query in RFC3339 format (e.g., '2024-01-01T13:00:00Z'), defaults to the current time. The end time must be within the last 30 days (string, optional)
  - `resolution`: Time resolution for data points in seconds. Lower values provide more granular data. Higher values provide more aggregated data points. API defaults to 60 seconds if not provided, minimum 30 seconds (number, optional)
  - `cpuUsageAggregationMethod`: Method for aggregating CPU usage metric values over time intervals. Accepted values: 'AVG', 'MAX', 'MIN', defaults to 'AVG' (string, optional)
  - `aggregateHttpRequestCountsBy`: Field to aggregate HTTP request count metrics by. Accepted values: 'host' (aggregate by request host), 'statusCode' (aggregate by HTTP status code). When not specified, returns total request counts (string, optional)
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
  - `plan`: Pricing plan for the database. Accepted values: 'free', 'basic_256mb', 'basic_1gb', 'basic_4gb', 'pro_4gb', 'pro_8gb', 'pro_16gb', 'pro_32gb', 'pro_64gb', 'pro_128gb', 'pro_192gb', 'pro_256gb', 'pro_384gb', 'pro_512gb', 'accelerated_16gb', 'accelerated_32gb', 'accelerated_64gb', 'accelerated_128gb', 'accelerated_256gb', 'accelerated_384gb', 'accelerated_512gb', 'accelerated_768gb', 'accelerated_1024gb' (string, required)
  - `region`: Region for deployment. Accepted values: 'oregon', 'frankfurt', 'singapore', 'ohio', 'virginia' (string, optional)
  - `version`: PostgreSQL version to use (e.g., 14, 15) (number, optional)
  - `diskSizeGb`: Database capacity in GB (number, optional)

### Key Value instances

- **list_key_value** - List all Key Value instances in your Render account
  - No parameters required

- **get_key_value** - Get details about a specific Key Value instance
  - `keyValueId`: The ID of the Key Value instance to retrieve (string, required)

- **create_key_value** - Create a new Key Value instance
  - `name`: Name of the Key Value instance (string, required)
  - `plan`: Pricing plan for the Key Value instance. Accepted values: 'free', 'starter', 'standard', 'pro', 'pro_plus' (string, required)
  - `region`: Region for deployment. Accepted values: 'oregon', 'frankfurt', 'singapore', 'ohio', 'virginia' (string, optional)
  - `maxmemoryPolicy`: Eviction policy for the Key Value store. Accepted values: 'noeviction', 'allkeys_lfu', 'allkeys_lru', 'allkeys_random', 'volatile_lfu', 'volatile_lru', 'volatile_random', 'volatile_ttl' (string, optional)

## Example Interactions

### Web Application Deployment

```
"Deploy my Node.js app called my-express-app using npm for build and start"
[MCP will set up a web service with appropriate npm commands]

"Show me recent logs for my-express-app"
```

### Database Setup & Management

```
"I need a PostgreSQL database for my users, call it user-db"
[MCP will create a PostgreSQL database with latest version]

"How do I connect to user-db?"
[MCP will show connection information]

"Set up a cache for my user data using a Key Value store"
[MCP will create a Key Value with appropriate configuration]
```

### Performance Monitoring & Optimization

```
"Show me CPU and memory metrics for my service srv-abc123 over the last 2 hours"
[MCP will fetch CPU and memory time-series data for analysis]

"Get HTTP request metrics for my web service to identify traffic patterns"
[MCP will show HTTP request volume and response metrics, or empty results if not a web service]

"Show me instance count metrics to understand my service scaling"
[MCP will display instance count over time to help with capacity planning and scaling decisions]

"Get HTTP error metrics to identify if issues are client-side or server-side problems"
[MCP will show error rates by status code, helping distinguish 4xx (client errors) vs 5xx (server errors)]

"Check response time metrics to debug performance issues"
[MCP will display P95 response time percentiles to identify latency bottlenecks and timeout issues]

"Check database performance metrics for my Postgres instance pg-def456"
[MCP will display CPU, memory, and connection metrics for the database]
```

## Troubleshooting

### Common Issues

1. **Connection Issues**
   - Verify your RENDER_API_KEY is correct
   - Check your internet connection
   - Verify Render.com API status

2. **Authorization Errors**
   - Check if your API key has expired
   - Ensure your API key is still valid and has not been revoked

3. **Service Creation Failures**
   - Verify repository URLs are accessible
   - Check that runtime and plan selections are valid
