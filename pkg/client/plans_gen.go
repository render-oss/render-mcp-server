// This file has been generated from our REST API schema. Do not edit it manually
// For more details, see public-api-schema/README.md.

// Code generated automatically. DO NOT EDIT.
package client

// PlanValues returns every valid Plan value defined in the
// public API schema.
func PlanValues() []Plan {
	return []Plan{
		Plan("starter"),
		Plan("starter_plus"),
		Plan("standard"),
		Plan("standard_plus"),
		Plan("pro"),
		Plan("pro_plus"),
		Plan("pro_max"),
		Plan("pro_ultra"),
		Plan("free"),
		Plan("custom"),
		Plan("starter_legacy"),
		Plan("standard_legacy"),
		Plan("standard_plus_legacy"),
		Plan("pro_legacy"),
		Plan("pro_plus_legacy"),
	}
}

// PaidPlanValues returns every valid PaidPlan value defined in the
// public API schema.
func PaidPlanValues() []PaidPlan {
	return []PaidPlan{
		PaidPlan("starter"),
		PaidPlan("standard"),
		PaidPlan("pro"),
		PaidPlan("pro_plus"),
		PaidPlan("pro_max"),
		PaidPlan("pro_ultra"),
	}
}

// KeyValuePlanValues returns every valid KeyValuePlan value defined in the
// public API schema.
func KeyValuePlanValues() []KeyValuePlan {
	return []KeyValuePlan{
		KeyValuePlan("free"),
		KeyValuePlan("starter"),
		KeyValuePlan("standard"),
		KeyValuePlan("pro"),
		KeyValuePlan("pro_plus"),
		KeyValuePlan("custom"),
	}
}

// RedisPlanValues returns every valid RedisPlan value defined in the
// public API schema.
func RedisPlanValues() []RedisPlan {
	return []RedisPlan{
		RedisPlan("free"),
		RedisPlan("starter"),
		RedisPlan("standard"),
		RedisPlan("pro"),
		RedisPlan("pro_plus"),
		RedisPlan("custom"),
	}
}
