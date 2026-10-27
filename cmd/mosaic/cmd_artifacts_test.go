package main

import (
	"context"
	"errors"
	"testing"
)

func TestSessionIDFromTmux(t *testing.T) {
	ctx := context.Background()
	var gotSocket, gotTarget string
	nameOf := func(_ context.Context, socket, target string) (string, error) {
		gotSocket, gotTarget = socket, target
		return "mos_01HZZ", nil
	}
	id, err := sessionIDFromTmux(ctx, "/private/tmp/tmux-501/mosaic,4242,3", nameOf)
	if err != nil || id != "01HZZ" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if gotSocket != "/private/tmp/tmux-501/mosaic" || gotTarget != "$3" {
		t.Fatalf("queried %q %q", gotSocket, gotTarget)
	}

	if _, err := sessionIDFromTmux(ctx, "", nameOf); err == nil {
		t.Fatal("empty TMUX must fail")
	}
	if _, err := sessionIDFromTmux(ctx, "garbage", nameOf); err == nil {
		t.Fatal("malformed TMUX must fail")
	}
	plain := func(context.Context, string, string) (string, error) { return "scratch", nil }
	if _, err := sessionIDFromTmux(ctx, "/tmp/s,1,0", plain); err == nil {
		t.Fatal("a non-mosaic session name must be rejected")
	}
	empty := func(context.Context, string, string) (string, error) { return "mos_", nil }
	if _, err := sessionIDFromTmux(ctx, "/tmp/s,1,0", empty); err == nil {
		t.Fatal("an empty id must be rejected")
	}
	failing := func(context.Context, string, string) (string, error) { return "", errors.New("no server") }
	if _, err := sessionIDFromTmux(ctx, "/tmp/s,1,0", failing); err == nil || err.Error() != "no server" {
		t.Fatalf("lookup error must propagate, got %v", err)
	}
}
