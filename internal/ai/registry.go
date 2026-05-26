package ai

import (
	"fmt"
	"sort"
	"strings"
)

// ProviderFactory builds a Provider from a generic config struct.
// Each adapter package registers itself via Register.
type ProviderFactory func(apiKey, baseURL, model string) Provider

var registry = map[string]ProviderFactory{}

// Register adds a named factory to the global registry.
// Call from each adapter's init() or explicitly before calling Build.
func Register(name string, f ProviderFactory) {
	registry[name] = f
}

// Build constructs the named provider from the supplied credentials.
// Returns an error with the list of valid names if the name is unknown,
// or if the provider requires an API key that is empty.
func Build(name, apiKey, baseURL, model string) (Provider, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q — valid providers: %s", name, validNames())
	}
	// local providers (ollama) do not require an API key
	if apiKey == "" && requiresKey(name) {
		return nil, fmt.Errorf("provider %q requires an API key — set it in config or as $AGTERM_%s_KEY",
			name, strings.ToUpper(name))
	}
	return f(apiKey, baseURL, model), nil
}

// ValidNames returns a sorted slice of registered provider names.
func ValidNames() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func validNames() string {
	return strings.Join(ValidNames(), ", ")
}

// requiresKey lists providers that need an API key to function.
func requiresKey(name string) bool {
	switch name {
	case "ollama":
		return false
	default:
		return true
	}
}
