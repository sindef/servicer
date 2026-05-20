package adapters

var knownServiceClasses = []ServiceClass{
	ServiceClassNamespace,
	ServiceClassPostgreSQL,
	ServiceClassMySQL,
	ServiceClassValkey,
	ServiceClassNATS,
	ServiceClassK8ssandra,
	ServiceClassYugabyte,
}

// KnownContracts returns the platform product contracts that Servicer recognizes.
func KnownContracts() []ProductContract {
	contracts := make([]ProductContract, 0, len(knownServiceClasses))
	for _, serviceClass := range knownServiceClasses {
		contract, ok := KnownContract(serviceClass)
		if ok {
			contracts = append(contracts, contract)
		}
	}
	return contracts
}

// KnownContract returns the static product contract for a recognized service class.
func KnownContract(serviceClass ServiceClass) (ProductContract, bool) {
	switch serviceClass {
	case ServiceClassNamespace:
		return NamespaceContract, true
	case ServiceClassPostgreSQL:
		return PostgreSQLContract, true
	case ServiceClassMySQL:
		return MySQLContract, true
	case ServiceClassValkey:
		return ValkeyContract, true
	case ServiceClassNATS:
		return NATSContract, true
	case ServiceClassK8ssandra:
		return K8ssandraContract, true
	case ServiceClassYugabyte:
		return YugabyteContract, true
	default:
		return ProductContract{}, false
	}
}
