package config

import (
	"budget/src/types"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ConfigSubdirUsesConfigDirAsBaseDir(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
	if err := os.Mkdir(filepath.Join(tmp, "config"), 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "config", "config.yaml"), []byte("server:\n  port: 9080\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	wantRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs wd: %v", err)
	}
	wantConfigDir := filepath.Join(wantRoot, "config")
	if cfg.BaseDir != wantConfigDir {
		t.Fatalf("expected config base dir %q, got %q", wantConfigDir, cfg.BaseDir)
	}
	if cfg.ConfigDir != wantConfigDir {
		t.Fatalf("expected config dir %q, got %q", wantConfigDir, cfg.ConfigDir)
	}
	if cfg.ConfigPath != filepath.Join(wantConfigDir, "config.yaml") {
		t.Fatalf("unexpected config path: %q", cfg.ConfigPath)
	}
}

func TestValidateRules_AllowsCommit(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{{
			ID:   "T1",
			Name: "测试包",
			Steps: []types.Step{{
				When: `项目 == "P1"`,
				Then: "commit",
			}},
		}},
	}
	if err := ValidateRules(cfg); err != nil {
		t.Fatalf("expected commit to validate: %v", err)
	}
}

func TestValidateRules_RejectsInvalidThen(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{{
			ID:   "T1",
			Name: "测试包",
			Steps: []types.Step{{
				Then: "invalid",
			}},
		}},
	}
	if err := ValidateRules(cfg); err == nil {
		t.Fatal("expected invalid then to fail validation")
	}
}
