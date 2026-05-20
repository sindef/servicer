package adapters

import "fmt"

// Registry stores the adapters available to the controller manager.
type Registry struct {
	adapters map[ServiceClass]ServiceAdapter
}

// NewRegistry builds a registry from the provided adapters.
func NewRegistry(adapters ...ServiceAdapter) (*Registry, error) {
	registry := &Registry{adapters: make(map[ServiceClass]ServiceAdapter, len(adapters))}
	for _, adapter := range adapters {
		if err := registry.Register(adapter); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds an adapter to the registry.
func (r *Registry) Register(adapter ServiceAdapter) error {
	contract := adapter.Contract()
	if contract.ServiceClass == "" {
		return fmt.Errorf("adapter has empty service class")
	}
	if _, exists := r.adapters[contract.ServiceClass]; exists {
		return fmt.Errorf("adapter already registered for service class %q", contract.ServiceClass)
	}
	r.adapters[contract.ServiceClass] = adapter
	return nil
}

// Get retrieves an adapter by service class.
func (r *Registry) Get(class ServiceClass) (ServiceAdapter, bool) {
	adapter, ok := r.adapters[class]
	return adapter, ok
}

// Contracts returns the registered product contracts.
func (r *Registry) Contracts() []ProductContract {
	contracts := make([]ProductContract, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		contracts = append(contracts, adapter.Contract())
	}
	return contracts
}
