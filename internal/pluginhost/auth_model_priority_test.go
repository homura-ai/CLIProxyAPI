package pluginhost

import (
	"reflect"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestPreserveFileAuthModelPriority(t *testing.T) {
	rules := map[string]any{"claude-fable-5-2": float64(10)}
	auth := &coreauth.Auth{
		Attributes: map[string]string{coreauth.AttributeSourceBackend: coreauth.AuthSourceFile, coreauth.AttributeFileModelPriority: "true"},
		Metadata:   map[string]any{coreauth.MetadataModelPriority: rules},
	}
	data := &pluginapi.AuthData{Metadata: map[string]any{coreauth.MetadataModelPriority: map[string]any{"*": float64(-10)}}}
	preserveFileAuthPriority(data, auth)
	if !reflect.DeepEqual(data.Metadata[coreauth.MetadataModelPriority], rules) {
		t.Fatalf("plugin refresh replaced file rules: %#v", data.Metadata)
	}
	if data.Attributes[coreauth.AttributeFileModelPriority] != "true" {
		t.Fatal("plugin refresh lost file marker")
	}
	storage := &pluginTokenStorage{provider: "claude", rawJSON: []byte(`{"type":"claude","model_priority":{"claude-fable-5-2":10}}`), meta: auth.Metadata}
	storage.SetMetadata(map[string]any{"type": "claude"})
	if ojson.GetBytes(storage.RawJSON(), coreauth.MetadataModelPriority).Exists() {
		t.Fatal("removed model_priority reappeared from raw plugin JSON")
	}
}
