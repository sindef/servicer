package adapters

import platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"

// PostgreSQLContract describes the normalized platform contract for CNPG-backed PostgreSQL.
var PostgreSQLContract = ProductContract{
	ServiceClass:            ServiceClassPostgreSQL,
	FriendlyName:            "PostgreSQL",
	RuntimeDriver:           "cnpg",
	SupportsVersionOverride: true,
	SupportsMultiCluster:    false,
	TopologyModes: []string{
		"single-cluster",
		"single-primary",
		"high-availability",
	},
	StatusSignals: []StatusSignalDescriptor{
		{Key: "primary-ready", DisplayName: "Primary Ready", Description: "Whether the writable PostgreSQL primary is ready.", Severity: HealthSeverityCritical},
		{Key: "replica-health", DisplayName: "Replica Health", Description: "Whether replica instances are healthy and attached.", Severity: HealthSeverityWarning},
		{Key: "backup-freshness", DisplayName: "Backup Freshness", Description: "Whether recent backups satisfy the selected backup policy.", Severity: HealthSeverityWarning},
	},
	Actions: []ActionCapability{
		{Name: ActionBackup, DisplayName: "Backup", RequiresApproval: false, Disruptive: false},
	},
}

// DefaultPostgreSQLDeletionPolicy is the preferred default for PostgreSQL instances.
const DefaultPostgreSQLDeletionPolicy = platformv1alpha1.DeletionPolicySnapshot
