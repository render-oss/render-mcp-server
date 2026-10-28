// This file has been generated from our REST API schema. Do not edit it manually
// For more details, see public-api-schema/README.md.

// Code generated automatically. DO NOT EDIT.
package client

// ManualEnvVarTargetTypeValues returns every valid ManualEnvVarTargetType value defined in the
// public API schema.
func ManualEnvVarTargetTypeValues() []ManualEnvVarTargetType {
	return []ManualEnvVarTargetType{
		ManualEnvVarTargetType("web"),
		ManualEnvVarTargetType("pserv"),
		ManualEnvVarTargetType("worker"),
		ManualEnvVarTargetType("cron"),
		ManualEnvVarTargetType("workflow"),
		ManualEnvVarTargetType("buildSource"),
	}
}
