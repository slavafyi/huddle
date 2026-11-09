package run

import (
	"context"
	"os"
	"testing"
)

func TestExecAddsEnv(t *testing.T) {
	t.Setenv("HUDDLE_INHERITED", "kept")
	out, err := Exec{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s %s' "$HUDDLE_INHERITED" "$HUDDLE_EXTRA"`},
		Env: []string{"HUDDLE_EXTRA=added"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "kept added" {
		t.Errorf("out = %q", out)
	}
	if os.Getenv("HUDDLE_EXTRA") != "" {
		t.Error("Env must not change huddle's own environment")
	}
}
