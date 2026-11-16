package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestFileTokenStoreModelPriority(t *testing.T) {
	base := t.TempDir()
	store := NewFileTokenStore()
	store.SetBaseDir(base)
	path := filepath.Join(base, "claude.json")
	for _, raw := range []string{`{"*":1.5}`, `null`, `[]`, `{"*":"10"}`} {
		data := []byte(`{"type":"claude","model_priority":` + raw + `}`)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if auths, err := store.readAuthFiles(path, base); err == nil || len(auths) != 0 {
			t.Fatalf("invalid file loaded: auths=%v error=%v", auths, err)
		}
	}
	auth := &coreauth.Auth{ID: "saved.json", FileName: "saved.json", Metadata: map[string]any{"type": "claude", coreauth.MetadataModelPriority: map[string]any{"*": 1.5}}}
	if _, err := store.Save(context.Background(), auth); err == nil {
		t.Fatal("saved invalid model_priority")
	}
	if _, err := os.Stat(filepath.Join(base, auth.FileName)); !os.IsNotExist(err) {
		t.Fatalf("invalid save touched disk: %v", err)
	}
	auth.Metadata[coreauth.MetadataModelPriority] = map[string]any{"claude-fable-5-2": 10}
	saved, err := store.Save(context.Background(), auth)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.readAuthFiles(saved, base)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("valid load: auths=%v error=%v", loaded, err)
	}
	rules, err := coreauth.ParseModelPriority(loaded[0].Metadata[coreauth.MetadataModelPriority])
	if err != nil || rules["claude-fable-5-2"] != 10 {
		t.Fatalf("saved rules=%v error=%v", rules, err)
	}
}
