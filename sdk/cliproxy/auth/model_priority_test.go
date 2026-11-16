package auth

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const priorityFableModel = "claude-fable-5-2"
const priorityOtherModel = "claude-opus-5-5"

func modelPriorityAccounts() (*Auth, *Auth) {
	return &Auth{
			ID: "a", Provider: "claude", Status: StatusActive,
			Attributes: map[string]string{"priority": "0"},
			Metadata:   map[string]any{MetadataModelPriority: map[string]any{priorityFableModel: float64(10)}},
		}, &Auth{
			ID: "b", Provider: "claude", Status: StatusActive,
			Attributes: map[string]string{"priority": "5"},
			Metadata:   map[string]any{MetadataModelPriority: map[string]any{"*fable*": float64(-10)}},
		}
}

func TestModelPriorityMatching(t *testing.T) {
	for _, test := range []struct {
		name, model, rules string
		want               int
	}{
		{"absent", priorityFableModel, "", 7},
		{"empty object", priorityFableModel, `{}`, 7},
		{"unmatched", priorityOtherModel, `{"*fable*":10}`, 7},
		{"empty model keeps base", "", `{"*":10}`, 7},
		{"exact beats wildcard", priorityFableModel, `{"claude-fable-5-2":-5,"*fable*":90}`, -5},
		{"longest wildcard", priorityFableModel, `{"*":90,"claude-*":20,"*fable*":-10}`, 20},
		{"case and whitespace", "  CLAUDE-FABLE-5-2  ", `{" CLAUDE-FABLE-5-2 ":-5}`, -5},
		{"thinking suffix", priorityFableModel + "(high)", `{"claude-fable-5-2":10}`, 10},
		{"multiple stars", priorityFableModel, `{"cl*de*fable*5*2":10}`, 10},
		{"anchored prefix", priorityFableModel, `{"fable*":10}`, 7},
		{"anchored suffix", priorityFableModel, `{"*fable":10}`, 7},
		{"lexical wildcard tie", "ab", `{"a*":20,"*b":-10}`, -10},
		{"lexical exact tie", "ab", `{"ab":20,"AB":-10}`, -10},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := &Auth{Attributes: map[string]string{"priority": "7"}, Metadata: map[string]any{}}
			if test.rules != "" {
				auth.Metadata[MetadataModelPriority] = ojson.Parse(test.rules).Value()
			}
			for i := 0; i < 100; i++ {
				if got := authPriorityForModel(auth, test.model); got != test.want {
					t.Fatalf("priority = %d, want %d", got, test.want)
				}
			}
		})
	}
}

func TestValidateAuthModelPriority(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `10`, `"high"`, `{"":1}`, `{"  ":1}`, `{"*":null}`, `{"*":true}`, `{"*":"10"}`, `{"*":1.5}`, `{"*":9223372036854775808}`} {
		t.Run(raw, func(t *testing.T) {
			metadata := map[string]any{MetadataModelPriority: ojson.Parse(raw).Value()}
			auth := &Auth{ID: "invalid", Provider: "claude", Metadata: metadata}
			if err := ValidateAuthModelPriority(auth); err == nil {
				t.Fatal("invalid rules accepted")
			}
			manager := NewManager(nil, nil, nil)
			if _, err := manager.Register(context.Background(), auth); err == nil {
				t.Fatal("manager registered invalid rules")
			}
			valid := &Auth{ID: auth.ID, Provider: auth.Provider}
			if _, err := manager.Register(context.Background(), valid); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Update(context.Background(), auth); err == nil {
				t.Fatal("manager updated to invalid rules")
			}
		})
	}
	for _, raw := range []any{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := ParseModelPriority(map[string]any{"*": raw}); err == nil {
			t.Fatalf("accepted non-finite value %v", raw)
		}
	}
	for _, raw := range []any{map[string]any{}, map[string]any{"*": -10, "model": float64(0)}, map[string]int{"*": 10}} {
		if _, err := ParseModelPriority(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModelPrioritySelectors(t *testing.T) {
	for _, selector := range []Selector{&RoundRobinSelector{}, &FillFirstSelector{}, &WeightedRoundRobinSelector{}} {
		for _, model := range []string{priorityFableModel, priorityOtherModel} {
			for _, state := range []string{"ready", "cooldown", "disabled", "unavailable"} {
				t.Run(strconv.Itoa(int(selectorStrategy(selector)))+"/"+model+"/"+state, func(t *testing.T) {
					a, b := modelPriorityAccounts()
					top, fallback := a, b
					if model == priorityOtherModel {
						top, fallback = b, a
					}
					want := top.ID
					switch state {
					case "cooldown":
						top.ModelStates = map[string]*ModelState{model: {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour), Quota: QuotaState{Exceeded: true}}}
					case "disabled":
						top.Disabled = true
					case "unavailable":
						top.Unavailable = true
						top.NextRetryAfter = time.Now().Add(time.Hour)
					}
					if state != "ready" {
						want = fallback.ID
					}
					picked, err := selector.Pick(context.Background(), "claude", model, cliproxyexecutor.Options{}, []*Auth{a, b})
					if err != nil || picked == nil || picked.ID != want {
						t.Fatalf("pick = %v, error = %v, want %s", picked, err, want)
					}
				})
			}
		}
	}
}

func registerPriorityModels(t *testing.T, auths ...*Auth) {
	t.Helper()
	for _, auth := range auths {
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: priorityFableModel}, {ID: priorityOtherModel}})
		id := auth.ID
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
}

func TestModelPriorityScheduler(t *testing.T) {
	for _, selector := range []Selector{&RoundRobinSelector{}, &FillFirstSelector{}, &WeightedRoundRobinSelector{}} {
		t.Run(strconv.Itoa(int(selectorStrategy(selector))), func(t *testing.T) {
			a, b := modelPriorityAccounts()
			registerPriorityModels(t, a, b)
			scheduler := newSchedulerForTest(selector, a, b)
			pick := func(model, want string, tried map[string]struct{}) {
				t.Helper()
				for _, mixed := range []bool{false, true} {
					var auth *Auth
					var err error
					if mixed {
						auth, _, err = scheduler.pickMixed(context.Background(), []string{"claude"}, model, cliproxyexecutor.Options{}, tried)
					} else {
						auth, err = scheduler.pickSingle(context.Background(), "claude", model, cliproxyexecutor.Options{}, tried)
					}
					if err != nil || auth == nil || auth.ID != want {
						t.Fatalf("model=%s mixed=%t: auth=%v error=%v, want %s", model, mixed, auth, err, want)
					}
				}
			}
			pick(priorityFableModel, a.ID, nil)
			pick(priorityOtherModel, b.ID, nil)
			pick(priorityFableModel, b.ID, map[string]struct{}{a.ID: {}})
			pick(priorityOtherModel, a.ID, map[string]struct{}{b.ID: {}})
			a.ModelStates = map[string]*ModelState{priorityFableModel: {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour), Quota: QuotaState{Exceeded: true}}}
			scheduler.upsertAuthResult(a, []string{priorityFableModel}, false)
			pick(priorityFableModel, b.ID, nil)
			pick(priorityOtherModel, b.ID, nil)
			a.ModelStates = nil
			scheduler.upsertAuthResult(a, []string{priorityFableModel}, false)
			pick(priorityFableModel, a.ID, nil)
			// A result scoped to another model must invalidate existing priority buckets.
			a.Metadata[MetadataModelPriority] = map[string]any{priorityFableModel: -20}
			scheduler.upsertAuthResult(a, []string{priorityOtherModel}, false)
			pick(priorityFableModel, b.ID, nil)
			delete(a.Metadata, MetadataModelPriority)
			scheduler.upsertAuth(a)
			pick(priorityFableModel, a.ID, nil)
			pick(priorityOtherModel, b.ID, nil)
			b.Disabled = true
			scheduler.upsertAuth(b)
			pick(priorityOtherModel, a.ID, nil)
		})
	}
}

func TestModelPriorityManagerAliasAndAffinity(t *testing.T) {
	withQuotaCooldownEnabled(t)
	for _, affinity := range []bool{false, true} {
		for _, alias := range []bool{false, true} {
			for _, weighted := range []bool{false, true} {
				t.Run(strconv.FormatBool(affinity)+"/alias="+strconv.FormatBool(alias)+"/weighted="+strconv.FormatBool(weighted), func(t *testing.T) {
					var selector Selector = &RoundRobinSelector{}
					if weighted {
						selector = &WeightedRoundRobinSelector{}
					}
					if affinity {
						sticky := NewSessionAffinitySelector(selector)
						t.Cleanup(sticky.Stop)
						selector = sticky
					}
					manager := NewManager(nil, selector, nil)
					manager.RegisterExecutor(schedulerTestExecutor{provider: "claude"})
					a, b := modelPriorityAccounts()
					fableRoute, otherRoute := priorityFableModel, priorityOtherModel
					if alias {
						a.Prefix, b.Prefix = "team", "team"
						fableRoute, otherRoute = "team/fable-route", "team/other-route"
						manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{"claude": {
							{Name: priorityFableModel, Alias: "fable-route"}, {Name: priorityOtherModel, Alias: "other-route"},
						}})
					}
					for _, auth := range []*Auth{a, b} {
						registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: fableRoute}, {ID: otherRoute}, {ID: priorityFableModel}, {ID: priorityOtherModel}})
						id := auth.ID
						t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
						if _, err := manager.Register(context.Background(), auth); err != nil {
							t.Fatal(err)
						}
					}
					pick := func(model, session, want string) {
						t.Helper()
						auth, err := manager.SelectAuth(context.Background(), "claude", model, cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": {session}}})
						if err != nil || auth == nil || auth.ID != want {
							t.Fatalf("model=%s session=%s: auth=%v error=%v, want %s", model, session, auth, err, want)
						}
					}
					pick(fableRoute, "stable", a.ID)
					pick(otherRoute, "stable", b.ID)
					retry := time.Hour
					manager.MarkResult(context.Background(), Result{AuthID: a.ID, Provider: "claude", Model: priorityFableModel, RetryAfter: &retry, Error: &Error{HTTPStatus: 429, Message: "quota"}})
					pick(fableRoute, "stable", b.ID)
					pick(otherRoute, "stable", b.ID)
					expireSessionAffinityPriorityModelCooldown(t, manager, a.ID, priorityFableModel)
					want := a.ID
					if affinity {
						want = b.ID
					}
					pick(fableRoute, "stable", want)
					pick(fableRoute, "fresh", a.ID)
				})
			}
		}
	}
}

func TestModelPriorityPluginSchedulerCandidates(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	a, b := modelPriorityAccounts()
	for _, test := range []struct {
		model string
		a, b  int
	}{{priorityFableModel, 10, -10}, {priorityOtherModel, 0, 5}} {
		candidates := manager.schedulerAuthCandidates([]*Auth{a, b}, test.model)
		if len(candidates) != 2 || candidates[0].Priority != test.a || candidates[1].Priority != test.b {
			t.Fatalf("model=%s: candidates=%+v", test.model, candidates)
		}
	}
}

func TestModelPriorityAPIKeyAliasStripsPrefixOnce(t *testing.T) {
	const target, route = "tenant/claude-fable-5-2", "tenant/public-model"
	for _, sticky := range []bool{false, true} {
		t.Run(strconv.FormatBool(sticky), func(t *testing.T) {
			var selector Selector = &WeightedRoundRobinSelector{}
			if sticky {
				affinity := NewSessionAffinitySelector(selector)
				t.Cleanup(affinity.Stop)
				selector = affinity
			}
			manager := NewManager(nil, selector, nil)
			manager.SetConfig(&internalconfig.Config{ClaudeKey: []internalconfig.ClaudeKey{
				{APIKey: "key-a", Prefix: "tenant", Models: []internalconfig.ClaudeModel{{Name: target, Alias: "public-model"}}},
				{APIKey: "key-b", Prefix: "tenant", Models: []internalconfig.ClaudeModel{{Name: target, Alias: "public-model"}}},
			}})
			manager.RegisterExecutor(schedulerTestExecutor{provider: "claude"})
			a, b := modelPriorityAccounts()
			for _, auth := range []*Auth{a, b} {
				auth.Prefix = "tenant"
				auth.Attributes["api_key"] = "key-" + auth.ID
				auth.Metadata[MetadataModelPriority] = map[string]any{target: 10}
				if auth == b {
					auth.Metadata[MetadataModelPriority] = map[string]any{target: -10}
				}
				registry.GetGlobalRegistry().RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: route}, {ID: target}})
				id := auth.ID
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				if _, err := manager.Register(context.Background(), auth); err != nil {
					t.Fatal(err)
				}
			}
			auth, err := manager.SelectAuth(context.Background(), "claude", route, cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": {"prefix-once"}}})
			if err != nil || auth == nil || auth.ID != a.ID {
				t.Fatalf("auth=%v error=%v, want %s", auth, err, a.ID)
			}
		})
	}
}

func TestModelPriorityExactAccountConfiguration(t *testing.T) {
	a, b := modelPriorityAccounts()
	b.Metadata[MetadataModelPriority] = map[string]any{priorityFableModel: -10}
	for _, model := range []string{priorityFableModel, priorityOtherModel, "claude-fable-5-1", "claude-sonnet-5-5"} {
		want := b.ID
		if model == priorityFableModel {
			want = a.ID
		}
		auth, err := (&FillFirstSelector{}).Pick(context.Background(), "claude", model, cliproxyexecutor.Options{}, []*Auth{a, b})
		if err != nil || auth == nil || auth.ID != want {
			t.Fatalf("model=%s auth=%v error=%v, want %s", model, auth, err, want)
		}
	}
}

func TestModelPriorityWeightedPoolSharesSchedulerAccounting(t *testing.T) {
	for _, size := range []int{3, maxExactWeightedPoolMembers + 2} {
		for _, prevalidated := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/prevalidated=%t", size, prevalidated), func(t *testing.T) {
				weighted := &WeightedRoundRobinSelector{}
				auths := make([]*Auth, size)
				for i := range auths {
					auths[i] = &Auth{
						ID: fmt.Sprintf("priority-pool-%04d", i), Provider: "claude",
						Attributes: map[string]string{"priority": strconv.Itoa(i % 2)},
						Metadata:   map[string]any{MetadataModelPriority: map[string]int{priorityFableModel: 100}},
					}
				}
				registerPriorityModels(t, auths...)
				scheduler := newSchedulerForTest(weighted, auths...)
				ctx, model := t.Context(), priorityFableModel
				if prevalidated {
					manager := NewManager(nil, weighted, nil)
					manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{"claude": {{Name: priorityFableModel, Alias: "public-model"}}})
					ctx = manager.selectorContextForAvailableAuths(ctx, weighted, "public-model")
					ctx = withWeightedSelectorStateModel(ctx, weighted, priorityFableModel)
					model = ""
				}
				first, err := weighted.Pick(ctx, "claude", model, cliproxyexecutor.Options{}, auths)
				if err != nil || first == nil || first.ID != auths[0].ID {
					t.Fatalf("first pick=%v err=%v, want %s", first, err, auths[0].ID)
				}
				second, err := scheduler.pickSingle(t.Context(), "claude", priorityFableModel, cliproxyexecutor.Options{}, nil)
				if err != nil || second == nil || second.ID != auths[1].ID {
					t.Fatalf("scheduler pick=%v err=%v, want %s", second, err, auths[1].ID)
				}
				remaining := auths
				if size > maxExactWeightedPoolMembers {
					// Large pools share a dynamic ledger across retry subsets.
					// Small exact pools key their ledgers by candidate membership.
					remaining = auths[1:]
				}
				third, err := weighted.Pick(ctx, "claude", model, cliproxyexecutor.Options{}, remaining)
				if err != nil || third == nil || third.ID != auths[2].ID {
					t.Fatalf("retry pick=%v err=%v, want %s", third, err, auths[2].ID)
				}
			})
		}
	}
}

func TestModelPrioritySchedulerRebuildPreservesRotation(t *testing.T) {
	a, b := modelPriorityAccounts()
	b.Metadata[MetadataModelPriority] = map[string]int{priorityFableModel: 10}
	registerPriorityModels(t, a, b)
	scheduler := newSchedulerForTest(&RoundRobinSelector{}, a, b)
	pick := func(want string) {
		t.Helper()
		auth, err := scheduler.pickSingle(t.Context(), "claude", priorityFableModel, cliproxyexecutor.Options{}, nil)
		if err != nil || auth == nil || auth.ID != want {
			t.Fatalf("pick=%v err=%v, want %s", auth, err, want)
		}
	}
	pick(a.ID)
	scheduler.rebuild([]*Auth{a, b})
	pick(b.ID)
	scheduler.rebuild([]*Auth{a, b})
	pick(a.ID)
	b.Metadata[MetadataModelPriority] = map[string]int{priorityFableModel: 20}
	scheduler.rebuild([]*Auth{a, b})
	pick(b.ID)
}

func TestModelPriorityPluginExclusionsAndDelegation(t *testing.T) {
	for _, delegate := range []string{"", pluginapi.SchedulerBuiltinRoundRobin, pluginapi.SchedulerBuiltinFillFirst} {
		for _, mixed := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("delegate=%s/mixed=%t/alias=%t", delegate, mixed, alias), func(t *testing.T) {
					sticky := NewSessionAffinitySelector(&RoundRobinSelector{})
					t.Cleanup(sticky.Stop)
					manager := NewManager(nil, sticky, nil)
					manager.RegisterExecutor(schedulerTestExecutor{provider: "claude"})
					a, b := modelPriorityAccounts()
					c := a.Clone()
					c.ID = "c"
					c.Metadata[MetadataModelPriority] = map[string]int{priorityFableModel: -20}
					route := priorityFableModel
					if alias {
						route = "public-model"
						manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{"claude": {{Name: priorityFableModel, Alias: route}}})
					}
					for _, auth := range []*Auth{a, b, c} {
						registerSchedulerModels(t, "claude", route, auth.ID)
						if _, err := manager.Register(t.Context(), auth); err != nil {
							t.Fatal(err)
						}
					}
					plugin := &fakePluginScheduler{acrossPriorities: true, handled: delegate != "", resp: pluginapi.SchedulerPickResponse{Handled: delegate != "", DelegateBuiltin: delegate}}
					manager.SetPluginScheduler(plugin)
					for _, excluded := range [][]string{nil, {a.ID}, {a.ID, b.ID}} {
						plugin.resp.ExcludeCandidates = excluded
						opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": {"plugin-priority"}}}
						var picked *Auth
						var err error
						if mixed {
							picked, _, _, err = manager.pickNextMixed(t.Context(), []string{"claude"}, route, opts, nil)
						} else {
							picked, _, err = manager.pickNext(t.Context(), "claude", route, opts, nil)
						}
						want := []*Auth{a, b, c}[len(excluded)].ID
						if err != nil || picked == nil || picked.ID != want {
							t.Fatalf("excluded=%v pick=%v err=%v, want %s", excluded, picked, err, want)
						}
					}
				})
			}
		}
	}
}
