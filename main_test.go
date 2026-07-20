package main

import (
	"flag"
	"reflect"
	"testing"
)

// testFlagSet mirrors the real subcommands: one flag that takes a value
// (add's --cmd) and one boolean (up's --wait-network).
func testFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("cmd", "", "")
	fs.Bool("wait-network", false, "")
	return fs
}

func TestPermute(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"flag after positional", []string{"path", "--cmd", "x"}, []string{"--cmd", "x", "path"}},
		{"already flags first", []string{"--cmd", "x", "path"}, []string{"--cmd", "x", "path"}},
		{"single dash form", []string{"path", "-cmd", "x"}, []string{"-cmd", "x", "path"}},
		{"inline value", []string{"path", "--cmd=x"}, []string{"--cmd=x", "path"}},
		{"bool keeps positional", []string{"name", "--wait-network"}, []string{"--wait-network", "name"}},
		{"bool mid-args", []string{"a", "b", "--wait-network", "c"}, []string{"--wait-network", "a", "b", "c"}},
		{"double dash ends flags", []string{"path", "--", "--cmd"}, []string{"path", "--cmd"}},
		{"unknown flag passed through", []string{"path", "-unknown"}, []string{"-unknown", "path"}},
		{"bare dash is positional", []string{"-", "--cmd", "x"}, []string{"--cmd", "x", "-"}},
		{"value flag at end without value", []string{"path", "--cmd"}, []string{"--cmd", "path"}},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := permute(testFlagSet(), tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("permute(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPermuteThroughParse is the case that was actually broken: the flag must
// reach the FlagSet and the path must stay a positional argument.
func TestPermuteThroughParse(t *testing.T) {
	fs := testFlagSet()
	if err := fs.Parse(permute(fs, []string{"/p", "--cmd", "docker compose up"})); err != nil {
		t.Fatal(err)
	}
	if got := fs.Lookup("cmd").Value.String(); got != "docker compose up" {
		t.Errorf("cmd = %q, want %q", got, "docker compose up")
	}
	if fs.NArg() != 1 || fs.Arg(0) != "/p" {
		t.Errorf("args = %q, want [/p]", fs.Args())
	}
}

func TestPermuteBoolThroughParse(t *testing.T) {
	fs := testFlagSet()
	if err := fs.Parse(permute(fs, []string{"myproject", "--wait-network"})); err != nil {
		t.Fatal(err)
	}
	if fs.Lookup("wait-network").Value.String() != "true" {
		t.Error("wait-network not set")
	}
	if fs.NArg() != 1 || fs.Arg(0) != "myproject" {
		t.Errorf("args = %q, want [myproject]", fs.Args())
	}
}
