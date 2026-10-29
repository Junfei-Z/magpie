package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DSH 0.2 imports settings.yaml into the profile it boots. An unused web
// profile can remain the empty template beside the configured desktop one.
func TestDshProfileReadSelection(t *testing.T) {
	native := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-flash\n    reasoningEffort: high\n"
	webPick := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n    reasoningEffort: low\n"
	for _, tc := range []struct {
		name, web, desktop, homePatch, settings string
		profile, model, effort                  string
	}{
		{name: "empty web template", web: dshDesktopPatch, desktop: native, profile: "desktop", model: "deepseek-flash", effort: "high"},
		{name: "configured web wins", web: webPick, desktop: native, profile: "web", model: "deepseek-v4-pro", effort: "low"},
		{name: "nonempty web is not an unused template", web: "- id: ui-settings-general\n  config:\n    theme: dark\n", desktop: native, profile: "web"},
		{name: "invalid web is unknown", web: "not a patch list\n", desktop: native, profile: "web"},
		{name: "no configured profile", web: dshDesktopPatch, desktop: dshDesktopPatch, profile: "web"},
		{name: "home model applies to web", web: dshDesktopPatch, desktop: native, homePatch: webPick, profile: "web", model: "deepseek-v4-pro", effort: "low"},
		{name: "legacy settings still apply to web", web: dshDesktopPatch, desktop: native, settings: "agent-default-model:\n  provider: deepseek-official\n  model: deepseek-v4-pro\n", profile: "web", model: "deepseek-v4-pro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dir, web, desktop := dshProfileReadHome(t, tc.web, tc.desktop)
			if tc.homePatch != "" {
				dshProfileReadWrite(t, filepath.Join(dir, "cordis.patch.yml"), tc.homePatch)
			}
			if tc.settings != "" {
				dshProfileReadWrite(t, filepath.Join(dir, "settings.yaml"), tc.settings)
			}
			a := dsh(home)
			if got := a.Field("model").Get(); got != tc.model {
				t.Errorf("model = %q, want %q", got, tc.model)
			}
			if got := a.Field("effort").Get(); got != tc.effort {
				t.Errorf("effort = %q, want %q", got, tc.effort)
			}
			wantPath := web
			if tc.profile == "desktop" {
				wantPath = desktop
			}
			if a.Path != wantPath {
				t.Errorf("config path = %q, want %q", a.Path, wantPath)
			}
			// Selecting a display source does not reorder the write targets.
			if files := DshPatchFiles(dir); len(files) != 2 || files[0] != web {
				t.Errorf("write targets changed: %v", files)
			}
			for path, want := range map[string]string{web: tc.web, desktop: tc.desktop} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, []byte(want)) {
					t.Errorf("reading changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestDshProfileReadUnreadableWeb(t *testing.T) {
	home, _, web, _ := dshProfileReadHome(t, dshDesktopPatch, "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-flash\n    reasoningEffort: high\n")
	if err := os.Remove(web); err != nil {
		t.Fatal(err)
	}
	// A directory at the patch path produces a read error even as root.
	if err := os.Mkdir(web, 0o700); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	if a.Path != web || a.Field("model").Get() != "" || a.Field("effort").Get() != "" {
		t.Fatalf("an unreadable web profile was treated as empty: path %q, values %v", a.Path, a.Values())
	}
}

func TestDshProfileReadJoinedAndCheck(t *testing.T) {
	route := "- id: llm-pi-ai\n  config:\n    providers:\n      magpie:\n        api: openai-completions\n        baseURL: https://wrong.test/v1\n        models:\n          - id: deepseek/flash\n"
	native := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-flash\n    reasoningEffort: high\n"
	home, _, web, desktop := dshProfileReadHome(t, dshDesktopPatch, route+native)
	a := dsh(home)
	if !a.Joined() {
		t.Fatal("the configured desktop's magpie route was not recognized")
	}
	dshProfileReadWrite(t, desktop, route+"- id: agent-default-model\n  config:\n    provider: magpie\n    model: deepseek/flash\n    reasoningEffort: high\n")
	if got := a.Field("model").Get(); got != "magpie/deepseek/flash" {
		t.Fatalf("desktop gateway model = %q", got)
	}
	if got := a.Check(); !strings.Contains(got, web) || !strings.Contains(got, "has none of magpie's models") {
		t.Fatalf("check did not retain the other profile's missing-route diagnostic: %q", got)
	}
}

func dshProfileReadHome(t *testing.T, webPatch, desktopPatch string) (home, dir, web, desktop string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir = filepath.Join(home, ".dsh")
	web = filepath.Join(dir, "profiles", "web", "cordis.patch.yml")
	desktop = filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml")
	dshProfileReadWrite(t, web, webPatch)
	dshProfileReadWrite(t, desktop, desktopPatch)
	return
}

func dshProfileReadWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
