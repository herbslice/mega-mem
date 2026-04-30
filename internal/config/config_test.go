package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEngineExpandsEnv(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("MM_TEST_VAULT", vault)
	t.Setenv("MM_TEST_KEY", "sekrit")

	cfg := filepath.Join(t.TempDir(), "engine.yaml")
	body := `
vault_path: ${MM_TEST_VAULT}
bind: 127.0.0.1:9999
embedding:
  provider: ollama
  endpoint: http://localhost:11434
  model: nomic-embed-text
  api_key: ${MM_TEST_KEY}
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	e, err := LoadEngine(cfg)
	if err != nil {
		t.Fatalf("LoadEngine: %v", err)
	}

	wantVault, _ := filepath.Abs(vault)
	if e.VaultPath != wantVault {
		t.Errorf("VaultPath = %q, want %q", e.VaultPath, wantVault)
	}
	if e.Embedding.APIKey != "sekrit" {
		t.Errorf("APIKey = %q, want %q", e.Embedding.APIKey, "sekrit")
	}
	if e.Bind != "127.0.0.1:9999" {
		t.Errorf("Bind = %q, want literal value preserved", e.Bind)
	}
}

func TestLoadEngineUnsetVarBecomesEmpty(t *testing.T) {
	// Unset env vars expand to empty (Bash/ExpandEnv semantics). vault_path
	// is required, so an unset variable for it surfaces as the existing
	// "vault_path is required" error rather than silently missing.
	vault := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "engine.yaml")
	body := "vault_path: " + vault + "\n" +
		"embedding:\n" +
		"  provider: ollama\n" +
		"  api_key: ${MM_TEST_DEFINITELY_NOT_SET}\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("MM_TEST_DEFINITELY_NOT_SET")
	e, err := LoadEngine(cfg)
	if err != nil {
		t.Fatalf("LoadEngine: %v", err)
	}
	if e.Embedding.APIKey != "" {
		t.Errorf("APIKey for unset var = %q, want empty", e.Embedding.APIKey)
	}
}

func TestLoadVaultExpandsEnv(t *testing.T) {
	t.Setenv("MM_TEST_REMOTE", "git@example.com:me/notes.git")
	vault := t.TempDir()
	body := `
vault_id: my-notes
commit:
  remote: ${MM_TEST_REMOTE}
`
	if err := os.WriteFile(filepath.Join(vault, ".mega-mem.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := LoadVault(vault)
	if err != nil {
		t.Fatalf("LoadVault: %v", err)
	}
	if v.Commit.Remote != "git@example.com:me/notes.git" {
		t.Errorf("Commit.Remote = %q, want expanded value", v.Commit.Remote)
	}
}

func TestLoadRegistryExpandsEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("MM_TEST_VAULT_DIR", "/tmp/somevault")

	regDir := filepath.Join(tmp, "mega-mem")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
vaults:
  mykb:
    path: ${MM_TEST_VAULT_DIR}
`
	if err := os.WriteFile(filepath.Join(regDir, "vaults.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	got, ok := r.Vaults["mykb"]
	if !ok {
		t.Fatalf("mykb not in registry")
	}
	if got.Path != "/tmp/somevault" {
		t.Errorf("Path = %q, want expanded value", got.Path)
	}
}
