package synthesizer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestSynthesizeAuthFileModelPriority(t *testing.T) {
	for _, rules := range []string{`null`, `[]`, `"bad"`, `{"*":1.5}`, `{"*":"10"}`, `{"":10}`} {
		t.Run(rules, func(t *testing.T) {
			ctx := &SynthesisContext{Config: &config.Config{}, AuthDir: t.TempDir(), Now: time.Now()}
			auths, err := SynthesizeAuthFile(ctx, filepath.Join(ctx.AuthDir, "invalid.json"), []byte(`{"type":"claude","model_priority":`+rules+`}`))
			if err == nil || len(auths) != 0 {
				t.Fatalf("auths=%v error=%v, want rejected credential", auths, err)
			}
		})
	}
	for _, plugin := range []bool{false, true} {
		ctx := &SynthesisContext{Config: &config.Config{}, AuthDir: t.TempDir(), Now: time.Now()}
		if plugin {
			ctx.PluginAuthParser = multiAuthParserFunc(func(context.Context, pluginapi.AuthParseRequest) ([]*coreauth.Auth, bool, error) {
				return []*coreauth.Auth{{ID: "plugin-a", Provider: "plugin"}, {ID: "plugin-b", Provider: "plugin"}}, true, nil
			})
		}
		auths, err := SynthesizeAuthFile(ctx, filepath.Join(ctx.AuthDir, "valid.json"), []byte(`{"type":"claude","model_priority":{"CLAUDE-FABLE-5-2":10}}`))
		if err != nil || len(auths) == 0 {
			t.Fatalf("plugin=%t auths=%v error=%v", plugin, auths, err)
		}
		for _, auth := range auths {
			if errValidate := coreauth.ValidateAuthModelPriority(auth); errValidate != nil {
				t.Fatal(errValidate)
			}
			if auth.Metadata[coreauth.MetadataModelPriority] == nil || auth.Attributes[coreauth.AttributeFileModelPriority] != "true" {
				t.Fatalf("plugin=%t lost file overrides: %+v", plugin, auth)
			}
		}
	}
}
