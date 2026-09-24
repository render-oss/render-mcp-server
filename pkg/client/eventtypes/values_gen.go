// This file has been generated from our REST API schema. Do not edit it manually
// For more details, see public-api-schema/README.md.

// Code generated automatically. DO NOT EDIT.
package client

// ServiceEventTypeValues returns every valid ServiceEventType value defined in the
// public API schema.
func ServiceEventTypeValues() []ServiceEventType {
	return []ServiceEventType{
		ServiceEventType("artifact_fetch_failed"),
		ServiceEventType("artifact_source_changed"),
		ServiceEventType("autoscaling_config_changed"),
		ServiceEventType("autoscaling_ended"),
		ServiceEventType("autoscaling_started"),
		ServiceEventType("branch_deleted"),
		ServiceEventType("build_ended"),
		ServiceEventType("build_started"),
		ServiceEventType("commit_ignored"),
		ServiceEventType("cron_job_run_ended"),
		ServiceEventType("cron_job_run_started"),
		ServiceEventType("deploy_ended"),
		ServiceEventType("deploy_started"),
		ServiceEventType("disk_created"),
		ServiceEventType("disk_updated"),
		ServiceEventType("disk_deleted"),
		ServiceEventType("service_disk_usage_high"),
		ServiceEventType("service_disk_usage_recovered"),
		ServiceEventType("image_pull_failed"),
		ServiceEventType("initial_deploy_hook_ended"),
		ServiceEventType("initial_deploy_hook_started"),
		ServiceEventType("instance_count_changed"),
		ServiceEventType("job_run_ended"),
		ServiceEventType("maintenance_mode_enabled"),
		ServiceEventType("maintenance_mode_uri_updated"),
		ServiceEventType("maintenance_ended"),
		ServiceEventType("maintenance_started"),
		ServiceEventType("pipeline_minutes_exhausted"),
		ServiceEventType("plan_changed"),
		ServiceEventType("pre_deploy_ended"),
		ServiceEventType("pre_deploy_started"),
		ServiceEventType("server_available"),
		ServiceEventType("server_failed"),
		ServiceEventType("server_hardware_failure"),
		ServiceEventType("server_restarted"),
		ServiceEventType("service_resumed"),
		ServiceEventType("service_suspended"),
		ServiceEventType("suspender_added"),
		ServiceEventType("suspender_removed"),
		ServiceEventType("zero_downtime_redeploy_ended"),
		ServiceEventType("zero_downtime_redeploy_started"),
		ServiceEventType("auto_deploy_disabled"),
		ServiceEventType("auto_deploy_enabled"),
	}
}
