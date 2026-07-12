package systemd

import (
	"strings"
	"testing"
)

func TestUnitContent(t *testing.T) {
	got := UnitContent("/home/u/.local/bin/keepon")
	for _, want := range []string{
		"ExecStart=/home/u/.local/bin/keepon up --wait-network",
		"WantedBy=default.target",
		"Type=oneshot",
		"RemainAfterExit=yes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unit file missing %q:\n%s", want, got)
		}
	}
}

func TestUnitPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	p, err := UnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/tmp/xdg/systemd/user/keepon.service" {
		t.Errorf("UnitPath() = %q", p)
	}
}
