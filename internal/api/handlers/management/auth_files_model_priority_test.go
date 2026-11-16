package management

import (
	"reflect"
	"testing"

	"github.com/MoeForge/moego/json"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestAuthFileModelPriorityFields(t *testing.T) {
	auth := &coreauth.Auth{ID: "model-priority.json", Attributes: map[string]string{coreauth.AttributeSourceBackend: coreauth.AuthSourceFile, "path": t.TempDir() + "/model-priority.json"}}
	patch := func(raw string) error {
		return applyAuthFileFieldsPatch(auth, map[string]json.RawMessage{coreauth.MetadataModelPriority: json.RawMessage(raw)}, authFileRequestRetryPatch{})
	}
	if err := patch(`{"claude-fable-5-2":10,"*fable*":-10}`); err != nil {
		t.Fatal(err)
	}
	rules, err := coreauth.ParseModelPriority(auth.Metadata[coreauth.MetadataModelPriority])
	if err != nil || rules["claude-fable-5-2"] != 10 || rules["*fable*"] != -10 {
		t.Fatalf("rules=%v error=%v", rules, err)
	}
	h := &Handler{}
	entry := h.buildAuthFileEntry(auth, authFileQuotaContext{v8: true})
	if !reflect.DeepEqual(entry[coreauth.MetadataModelPriority], rules) {
		t.Fatalf("listing rules = %#v, want %#v", entry[coreauth.MetadataModelPriority], rules)
	}
	if err := patch(`null`); err != nil {
		t.Fatal(err)
	}
	if _, exists := auth.Metadata[coreauth.MetadataModelPriority]; exists {
		t.Fatal("null patch kept overrides")
	}
	if _, exists := auth.Attributes[coreauth.AttributeFileModelPriority]; exists {
		t.Fatal("null patch kept file marker")
	}
	for _, raw := range []string{`[]`, `"bad"`, `{"*":1.5}`, `{"*":"10"}`, `{"":10}`} {
		if err := patch(raw); err == nil {
			t.Fatalf("patch %s accepted invalid rules", raw)
		}
	}
	if err := applyAuthFileFieldsPatch(auth, map[string]json.RawMessage{"model_priority.child": json.RawMessage(`10`)}, authFileRequestRetryPatch{}); err == nil {
		t.Fatal("nested patch accepted")
	}
}
