package provider

import (
	"context"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"
)

// Router implements Provider by routing requests to the most appropriate
// backend based on task complexity, cost budget, and user overrides.
type Router struct {
	providers map[string]Provider
	rules     []RoutingRule
	fallback  string // Default provider name
}

// RoutingRule maps task characteristics to a provider/model choice.
type RoutingRule struct {
	Name        string
	Condition   func(req *Request) bool
	Provider    string
	Model       string
	Description string
}

// NewRouter creates a router with the given providers and default.
func NewRouter(fallback string, providers ...Provider) *Router {
	r := &Router{
		providers: make(map[string]Provider),
		fallback:  fallback,
	}

	for _, p := range providers {
		r.providers[p.Name()] = p
	}

	// Default routing rules (can be customized)
	r.rules = []RoutingRule{
		{
			Name:        "simple-query",
			Description: "Short questions without tools → cheaper model",
			Condition: func(req *Request) bool {
				if len(req.Messages) == 0 {
					return false
				}
				last := req.Messages[len(req.Messages)-1].Content
				return utf8.RuneCountInString(last) < 100 && len(req.Tools) == 0
			},
			Provider: "anthropic",
			Model:    "claude-haiku-4-20250414",
		},
		{
			Name:        "code-generation",
			Description: "Requests involving code → capable model",
			Condition: func(req *Request) bool {
				if len(req.Messages) == 0 {
					return false
				}
				last := strings.ToLower(req.Messages[len(req.Messages)-1].Content)
				codeWords := []string{"write", "implement", "create", "build", "code", "function", "class", "refactor"}
				for _, w := range codeWords {
					if strings.Contains(last, w) {
						return true
					}
				}
				return false
			},
			Provider: "anthropic",
			Model:    "claude-sonnet-4-20250514",
		},
	}

	return r
}

func (r *Router) Name() string { return "router" }

func (r *Router) Models() []string {
	var models []string
	for _, p := range r.providers {
		models = append(models, p.Models()...)
	}
	return models
}

func (r *Router) Complete(ctx context.Context, req *Request) (*Response, error) {
	p, model := r.route(req)
	if model != "" && req.Model == "" {
		req.Model = model
	}
	return p.Complete(ctx, req)
}

func (r *Router) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	p, model := r.route(req)
	if model != "" && req.Model == "" {
		req.Model = model
	}
	return p.Stream(ctx, req)
}

// route selects the best provider and model for a request.
func (r *Router) route(req *Request) (Provider, string) {
	// User override always wins
	if req.Model != "" {
		// Find which provider serves this model
		for _, p := range r.providers {
			for _, m := range p.Models() {
				if m == req.Model {
					log.Printf("router: user override → %s/%s", p.Name(), req.Model)
					return p, req.Model
				}
			}
		}
	}

	// Apply rules in order
	for _, rule := range r.rules {
		if rule.Condition(req) {
			if p, ok := r.providers[rule.Provider]; ok {
				log.Printf("router: rule %q matched → %s/%s", rule.Name, rule.Provider, rule.Model)
				return p, rule.Model
			}
		}
	}

	// Fallback
	if p, ok := r.providers[r.fallback]; ok {
		return p, ""
	}

	// Last resort: first available provider
	for _, p := range r.providers {
		return p, ""
	}

	return nil, ""
}

// AddRule adds a custom routing rule.
func (r *Router) AddRule(rule RoutingRule) {
	r.rules = append([]RoutingRule{rule}, r.rules...) // Prepend (higher priority)
}

// Providers returns the list of available provider names.
func (r *Router) Providers() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// SetProvider adds or replaces a provider.
func (r *Router) SetProvider(p Provider) {
	r.providers[p.Name()] = p
}

// GetProvider returns a specific provider by name.
func (r *Router) GetProvider(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}
