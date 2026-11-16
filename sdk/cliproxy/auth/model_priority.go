package auth

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/modelpattern"
)

// MetadataModelPriority names per-model priority overrides in credential metadata.
const MetadataModelPriority = "model_priority"

// ParseModelPriority validates a model-pattern-to-integer object. Patterns are
// retained verbatim for persistence and deterministic lexical tie breaking.
func ParseModelPriority(value any) (map[string]int, error) {
	priorities := make(map[string]int)
	switch entries := value.(type) {
	case map[string]any:
		for pattern, raw := range entries {
			priority, errParse := parseModelPriorityInt(raw)
			if errParse != nil {
				return nil, fmt.Errorf("model_priority[%q]: %w", pattern, errParse)
			}
			priorities[pattern] = priority
		}
	case map[string]int:
		for pattern, priority := range entries {
			priorities[pattern] = priority
		}
	default:
		return nil, fmt.Errorf("model_priority must be an object mapping model patterns to integers")
	}
	for pattern := range priorities {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("model_priority patterns must not be empty")
		}
	}
	return priorities, nil
}

func parseModelPriorityInt(value any) (int, error) {
	switch number := value.(type) {
	case int:
		return number, nil
	case int64:
		if int64(int(number)) == number {
			return int(number), nil
		}
	case float64:
		// The positive bound is exclusive: float64(MaxInt) rounds up on 64-bit systems.
		limit := math.Ldexp(1, strconv.IntSize-1)
		if !math.IsNaN(number) && !math.IsInf(number, 0) && math.Trunc(number) == number && number >= -limit && number < limit {
			return int(number), nil
		}
	case json.Number:
		parsed, errParse := strconv.ParseInt(string(number), 10, strconv.IntSize)
		if errParse == nil {
			return int(parsed), nil
		}
	}
	return 0, fmt.Errorf("priority must be an integer in the platform int range")
}

// ValidateAuthModelPriority rejects invalid explicit overrides at credential
// load and mutation boundaries, following credential weight validation.
func ValidateAuthModelPriority(auth *Auth) error {
	if auth == nil {
		return nil
	}
	if raw, exists := auth.Metadata[MetadataModelPriority]; exists {
		_, errParse := ParseModelPriority(raw)
		return errParse
	}
	return nil
}

// authPriorityForModel uses exact matches first, then the longest matching
// wildcard. Equal lengths (including case variants) use lexical key order.
func authPriorityForModel(auth *Auth, model string) int {
	priority := authPriority(auth)
	if auth == nil || strings.TrimSpace(model) == "" {
		return priority
	}
	raw, exists := auth.Metadata[MetadataModelPriority]
	if !exists {
		return priority
	}
	priorities, errParse := ParseModelPriority(raw)
	if errParse != nil {
		return priority
	}
	model = canonicalModelLookupKey(model)
	bestPattern, bestKey := "", ""
	bestExact := false
	for key, candidatePriority := range priorities {
		pattern := strings.ToLower(strings.TrimSpace(key))
		exact := !strings.Contains(pattern, "*")
		if !modelpattern.Match(pattern, model) {
			continue
		}
		if bestKey == "" || (exact && !bestExact) || (exact == bestExact && (len(pattern) > len(bestPattern) || (len(pattern) == len(bestPattern) && key < bestKey))) {
			bestPattern, bestKey, bestExact = pattern, key, exact
			priority = candidatePriority
		}
	}
	return priority
}

type priorityModelResolverKey struct{}

// withPriorityModelResolver preserves per-auth model resolution even when the
// built-in selector receives an empty availability model for prevalidated auths.
func (m *Manager) withPriorityModelResolver(ctx context.Context, routeModel string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, priorityModelResolverKey{}, func(auth *Auth) string {
		return m.priorityModelForAuth(auth, routeModel)
	})
}

// priorityModelForAuth resolves the same upstream model identity as normal
// routing without advancing model-pool cursors during candidate comparison.
func (m *Manager) priorityModelForAuth(auth *Auth, routeModel string) string {
	resolved := m.selectionModelForAuth(auth, routeModel)
	if target := m.applyAPIKeyModelAlias(auth, resolved); strings.TrimSpace(target) != "" {
		return target
	}
	return resolved
}

func authPriorityForRequest(ctx context.Context, auth *Auth, model string) int {
	if ctx != nil {
		if resolve, ok := ctx.Value(priorityModelResolverKey{}).(func(*Auth) string); ok {
			return authPriorityForModel(auth, resolve(auth))
		}
	}
	return authPriorityForModel(auth, rewriteModelForAuth(model, auth))
}
