package adapters

import platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"

// K8ssandraContract describes the normalized platform contract for Cassandra services.
var K8ssandraContract = ProductContract{
	ServiceClass:            ServiceClassK8ssandra,
	FriendlyName:            "Cassandra",
	RuntimeDriver:           "k8ssandra",
	SupportsVersionOverride: true,
	SupportsMultiCluster:    true,
	TopologyModes: []string{
		"single-datacenter",
		"multi-datacenter",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "ring-health", DisplayName: "Ring Health", Description: "Whether the Cassandra ring is healthy and converged.", Severity: HealthSeverityCritical},
		{Key: "repair-posture", DisplayName: "Repair Posture", Description: "Whether the cluster is within its repair and anti-entropy expectations.", Severity: HealthSeverityWarning},
		{Key: "backup-freshness", DisplayName: "Backup Freshness", Description: "Whether backups satisfy the plan's retention and recovery expectations.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionScale, DisplayName: "Scale", RequiresApproval: false, Disruptive: true},
		{Name: ActionRestart, DisplayName: "Rolling Restart", RequiresApproval: false, Disruptive: true},
		{Name: ActionRepair, DisplayName: "Repair", RequiresApproval: true, Disruptive: true},
		{Name: ActionBackup, DisplayName: "Backup", RequiresApproval: false, Disruptive: false},
	},
}

// DefaultK8ssandraDeletionPolicy is the preferred default for Cassandra instances.
const DefaultK8ssandraDeletionPolicy = platformv1alpha1.DeletionPolicySnapshot
