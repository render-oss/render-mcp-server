package service

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	serviceRepo := NewRepo(c)

	return []server.ServerTool{
		listServices(serviceRepo),
		getService(serviceRepo),
		createWebService(serviceRepo),
		createStaticSite(serviceRepo),
		updateWebService(),
		updateStaticSite(),
		updateEnvVars(serviceRepo),
	}
}

func listServices(serviceRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_services",
			mcp.WithDescription("List all services in your Render account"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:         "List services",
				ReadOnlyHint:  pointers.From(true),
				OpenWorldHint: pointers.From(true),
			}),
			mcp.WithBoolean("includePreviews",
				mcp.Description("Whether to include preview services in the response. Defaults to false."),
				mcp.DefaultBool(false),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			params := &client.ListServicesParams{}

			if includePreviews, ok, err := validate.OptionalToolParam[bool](request, "includePreviews"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.IncludePreviews = &includePreviews
			}

			response, err := serviceRepo.ListServices(ctx, params)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func getService(serviceRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_service",
			mcp.WithDescription("Get details about a specific service"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:         "Get service details",
				ReadOnlyHint:  pointers.From(true),
				OpenWorldHint: pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to retrieve"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := request.RequireString("serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			response, err := serviceRepo.GetService(ctx, serviceId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func createWebService(serviceRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("create_web_service",
			mcp.WithDescription("Create a new web service in your Render account. "+
				"A web service is a public-facing service that can be accessed by users on the internet. "+
				"By default, these services are automatically deployed when the specified branch is updated "+
				"and do not require a manual trigger of a deploy. The user should only be prompted to manually trigger a deploy if auto-deploy is disabled."+
				"This tool is currently limited to support only a subset of the web service configuration parameters."+
				"It also only supports web services which don't use Docker, or a container registry."+
				"To create a service without those limitations, please use the dashboard at: "+config.DashboardURL()+"/web/new"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Create web service",
				ReadOnlyHint:   pointers.From(false),
				IdempotentHint: pointers.From(false),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("A unique name for your service. This will be used to generate the service's URL if it is public."),
			),
			mcp.WithString("repo",
				mcp.Description("The repository containing the source code for your service. Must be a valid Git URL that Render can clone and deploy. Do not include the branch in the repo string. You can instead supply a 'branch' parameter."),
			),
			mcp.WithString("branch",
				mcp.Description("The repository branch to deploy. This branch will be deployed when you manually trigger deploys and when auto-deploy is enabled. If left empty, this will fall back to the default branch of the repository."),
			),
			mcp.WithString("autoDeploy",
				mcp.Description("Whether to automatically deploy the service when the specified branch is updated. Defaults to 'yes'."),
				mcp.Enum(string(client.AutoDeployYes), string(client.AutoDeployNo)),
				mcp.DefaultString(string(client.AutoDeployYes)),
			),
			mcp.WithString("runtime",
				mcp.Required(),
				mcp.Description("The runtime environment for your service. This determines how your service is built and run."),
				mcp.Enum("node", "python", "go", "rust", "ruby", "elixir", "docker"),
			),
			mcp.WithString("plan",
				mcp.Description("The pricing plan for your service. Different plans offer different levels of resources and features."),
				mcp.Enum(mcpserver.EnumValuesFromClientType(client.PaidPlanStarter, client.PaidPlanStandard, client.PaidPlanPro, client.PaidPlanProMax, client.PaidPlanProPlus, client.PaidPlanProUltra)...),
				mcp.DefaultString(string(client.PaidPlanStarter)),
			),
			mcp.WithString("buildCommand",
				mcp.Required(),
				mcp.Description("The command used to build your service. For example, 'npm run build' for Node.js or 'pip install -r requirements.txt' for Python."),
			),
			mcp.WithString("startCommand",
				mcp.Required(),
				mcp.Description("The command used to start your service. For example, 'npm start' for Node.js or 'gunicorn app:app' for Python."),
			),
			mcp.WithString("region",
				mcp.Description("The geographic region where your service will be deployed. Defaults to Oregon. Choose the region closest to your users for best performance."),
				mcp.Enum(mcpserver.RegionEnumValues()...),
				mcp.DefaultString(string(client.Oregon)),
			),
			mcp.WithArray("envVars",
				mcp.Description("Environment variables to set for your service. These are exposed during builds and at runtime."),
				mcp.Items(
					map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"key", "value"},
						"properties": map[string]interface{}{
							"key": map[string]interface{}{
								"type":        "string",
								"description": "The name of the environment variable",
							},
							"value": map[string]interface{}{
								"type":        "string",
								"description": "The value of the environment variable",
							},
						},
					},
				),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			requestBody, err := createValidatedWebServiceRequest(ctx, request)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			response, err := serviceRepo.CreateService(ctx, *requestBody)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func createValidatedWebServiceRequest(ctx context.Context, request mcp.CallToolRequest) (*client.CreateServiceJSONRequestBody, error) {
	runtime, err := validate.RequiredToolParam[string](request, "runtime")
	if err != nil {
		return nil, err
	}

	buildCommand, err := validate.RequiredToolParam[string](request, "buildCommand")
	if err != nil {
		return nil, err
	}

	startCommand, err := validate.RequiredToolParam[string](request, "startCommand")
	if err != nil {
		return nil, err
	}

	nativeEnvironmentDetails := client.NativeEnvironmentDetailsPOST{
		BuildCommand: buildCommand,
		StartCommand: startCommand,
	}

	envSpecificDetails := client.EnvSpecificDetailsPOST{}
	if err = envSpecificDetails.FromNativeEnvironmentDetailsPOST(nativeEnvironmentDetails); err != nil {
		return nil, err
	}

	webServiceDetailsPOST := client.WebServiceDetailsPOST{
		Runtime:            client.ServiceRuntime(runtime),
		EnvSpecificDetails: &envSpecificDetails,
	}

	if plan, ok, err := validate.OptionalToolParam[string](request, "plan"); err != nil {
		return nil, err
	} else if ok {
		paidPlan, err := validate.PaidPlan(plan)
		if err != nil {
			return nil, err
		}
		webServiceDetailsPOST.Plan = paidPlan
	}

	if region, ok, err := validate.OptionalToolParam[string](request, "region"); err != nil {
		return nil, err
	} else if ok {
		webServiceDetailsPOST.Region = (*client.Region)(&region)
	}

	serviceDetails := client.ServicePOST_ServiceDetails{}
	if err = serviceDetails.FromWebServiceDetailsPOST(webServiceDetailsPOST); err != nil {
		return nil, err
	}

	return validatedCreateServiceRequest(ctx, request, client.WebService, &serviceDetails)
}

func validatedCreateServiceRequest(ctx context.Context, request mcp.CallToolRequest, serviceType client.ServiceType, serviceDetails *client.ServicePOST_ServiceDetails) (*client.CreateServiceJSONRequestBody, error) {
	name, err := validate.RequiredToolParam[string](request, "name")
	if err != nil {
		return nil, err
	}
	ownerId, err := session.FromContext(ctx).GetWorkspace()
	if err != nil {
		return nil, err
	}

	requestBody := &client.CreateServiceJSONRequestBody{
		Name:           name,
		OwnerId:        ownerId,
		Type:           serviceType,
		ServiceDetails: serviceDetails,
	}

	if repo, ok, err := validate.OptionalToolParam[string](request, "repo"); err != nil {
		return nil, err
	} else if ok {
		requestBody.Repo = &repo
	}

	if branch, ok, err := validate.OptionalToolParam[string](request, "branch"); err != nil {
		return nil, err
	} else if ok {
		requestBody.Branch = &branch
	}

	if autoDeploy, ok, err := validate.OptionalToolParam[string](request, "autoDeploy"); err != nil {
		return nil, err
	} else if ok {
		requestBody.AutoDeploy = (*client.AutoDeploy)(&autoDeploy)
	}

	if envVars, ok, err := validate.EnvVars(request); err != nil {
		return nil, err
	} else if ok {
		requestBody.EnvVars = &envVars
	}

	return requestBody, nil
}

func createStaticSite(serviceRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("create_static_site",
			mcp.WithDescription("Create a new static site in your Render account. "+
				"Apps that consist entirely of statically served assets (commonly HTML, CSS, and JS). "+
				"Static sites have a public onrender.com subdomain and are served over a global CDN. "+
				"Create a static site if you're building with a framework like: Create React App, Vue.js, Gatsby, etc."+
				"This tool is currently limited to support only a subset of the static site configuration parameters."+
				"To create a static site without those limitations, please use the dashboard at: "+config.DashboardURL()+"/static/new"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Create static site",
				ReadOnlyHint:   pointers.From(false),
				IdempotentHint: pointers.From(false),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("A unique name for your service. This will be used to generate the service's URL if it is public."),
			),
			mcp.WithString("repo",
				mcp.Description("The repository containing the source code for your service. Must be a valid Git URL that Render can clone and deploy. Do not include the branch in the repo string. You can instead supply a 'branch' parameter."),
			),
			mcp.WithString("branch",
				mcp.Description("The repository branch to deploy. This branch will be deployed when you manually trigger deploys and when auto-deploy is enabled. If left empty, this will fall back to the default branch of the repository."),
			),
			mcp.WithString("autoDeploy",
				mcp.Description("Whether to automatically deploy the service when the specified branch is updated. Defaults to 'yes'."),
				mcp.Enum(string(client.AutoDeployYes), string(client.AutoDeployNo)),
				mcp.DefaultString(string(client.AutoDeployYes)),
			),
			mcp.WithString("buildCommand",
				mcp.Required(),
				mcp.Description("Render runs this command to build your app before each deploy. For example, 'yarn; yarn build' a React app."),
			),
			mcp.WithString("publishPath",
				mcp.Description("The relative path of the directory containing built assets to publish. Examples: ./, ./build, dist and frontend/build. This is the directory that will be served to the public."),
				mcp.DefaultString("public"),
			),
			mcp.WithArray("envVars",
				mcp.Description("Environment variables to set for your service. These are exposed during builds and at runtime."),
				mcp.Items(
					map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"key", "value"},
						"properties": map[string]interface{}{
							"key": map[string]interface{}{
								"type":        "string",
								"description": "The name of the environment variable",
							},
							"value": map[string]interface{}{
								"type":        "string",
								"description": "The value of the environment variable",
							},
						},
					},
				),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			requestBody, err := createValidatedStaticSiteRequest(ctx, request)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			response, err := serviceRepo.CreateService(ctx, *requestBody)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func createValidatedStaticSiteRequest(ctx context.Context, request mcp.CallToolRequest) (*client.CreateServiceJSONRequestBody, error) {
	buildCommand, err := validate.RequiredToolParam[string](request, "buildCommand")
	if err != nil {
		return nil, err
	}

	staticSiteDetailsPOST := client.StaticSiteDetailsPOST{
		BuildCommand: &buildCommand,
	}

	if publishPath, ok, err := validate.OptionalToolParam[string](request, "publishPath"); err != nil {
		return nil, err
	} else if ok {
		staticSiteDetailsPOST.PublishPath = &publishPath
	}

	serviceDetails := client.ServicePOST_ServiceDetails{}
	if err = serviceDetails.FromStaticSiteDetailsPOST(staticSiteDetailsPOST); err != nil {
		return nil, err
	}

	return validatedCreateServiceRequest(ctx, request, client.StaticSite, &serviceDetails)
}

func updateWebService() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("update_web_service",
			mcp.WithDescription("Update an existing web service in your Render account."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Update web service",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to update"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Return a message indicating direct updates are not supported via MCP server
			return mcp.NewToolResultText(
				"Updating a service directly is not supported. Please make changes using the dashboard or the API.\n\n" +
					"Dashboard URL: " + config.DashboardURL() + "/web/" + serviceId + "/settings"), nil
		},
	}
}

func updateStaticSite() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("update_static_site",
			mcp.WithDescription("Update an existing static site in your Render account."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Update static site",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to update"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Return a message indicating direct updates are not supported via MCP server
			return mcp.NewToolResultText(
				"Updating a static site directly is not supported. Please make changes using the dashboard or the API.\n\n" +
					"Dashboard URL: " + config.DashboardURL() + "/static/" + serviceId + "/settings"), nil
		},
	}
}

func updateEnvVars(serviceRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("update_environment_variables",
			mcp.WithDescription("Update environment variables for a service. "+
				"By default, environment variables passed in will be merged with the service's "+
				"existing environment variables. This makes it safe to update environment variables"+
				"without pulling the existing ones into the MCP host's context. "+
				"To replace all existing environment variables, set the 'replace' parameter to 'true'."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Update environment variables",
				DestructiveHint: pointers.From(true),
				OpenWorldHint:   pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to update"),
			),
			mcp.WithBoolean("replace",
				mcp.Description("Whether to replace all existing environment variables with the "+
					"provided list, or merge with the existing ones. Defaults to false."),
				mcp.DefaultBool(false),
			),
			mcp.WithArray("envVars",
				mcp.Required(),
				mcp.Description("The list of environment variables to update or set for the service."),
				mcp.Items(
					map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"key", "value"},
						"properties": map[string]interface{}{
							"key": map[string]interface{}{
								"type":        "string",
								"description": "The name of the environment variable",
							},
							"value": map[string]interface{}{
								"type":        "string",
								"description": "The value of the environment variable",
							},
						},
					},
				),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			var envVars []client.EnvVarInput
			var ok bool
			if envVars, ok, err = validate.EnvVars(request); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if !ok {
				return mcp.NewToolResultError("Environment variables are required"), nil
			}

			var envVarsToSet []client.EnvVarInput

			replace, _, err := validate.OptionalToolParam[bool](request, "replace")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if replace {
				envVarsToSet = envVars
			} else {
				oldEnvVars, err := serviceRepo.ListEnvVars(ctx, serviceId, &client.GetEnvVarsForServiceParams{})
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				envVarsToSet, err = mergeEnvVars(oldEnvVars, envVars)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}

			_, err = serviceRepo.UpdateEnvVars(ctx, serviceId, envVarsToSet)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Now trigger a deploy so that the updated environment variables are picked up
			deployResponse, err := serviceRepo.DeployService(ctx, serviceId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			responseText := "Environment variables updated. A new deploy has been triggered to pick up the changes.\n\n"
			responseText += "Response from deploying service: " + string(deployResponse.Body)

			return mcp.NewToolResultText(responseText), nil
		},
	}
}

func mergeEnvVars(oldEnvVars []*client.EnvVar, envVarInputs []client.EnvVarInput) ([]client.EnvVarInput, error) {
	envVarMap := make(map[string]string)
	for _, envVar := range oldEnvVars {
		envVarMap[envVar.Key] = envVar.Value
	}

	for _, envVarInput := range envVarInputs {
		envVar, err := envVarInput.AsEnvVarKeyValue()
		if err != nil {
			return nil, err
		}
		envVarMap[envVar.Key] = envVar.Value
	}

	// Convert map back to list
	var mergedEnvVars []client.EnvVarInput
	for k, v := range envVarMap {
		var envVarInput client.EnvVarInput
		err := envVarInput.FromEnvVarKeyValue(client.EnvVarKeyValue{
			Key:   k,
			Value: v,
		})
		if err != nil {
			return nil, err
		}
		mergedEnvVars = append(mergedEnvVars, envVarInput)
	}

	return mergedEnvVars, nil
}
