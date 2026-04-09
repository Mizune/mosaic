package hook_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Mizune/mosaic/internal/bus"
	"github.com/Mizune/mosaic/internal/hook"
)

func TestServer_BusyMessage(t *testing.T) {
	b := bus.New()
	defer b.Close()

	got := make(chan any, 1)
	b.Subscribe(func(e bus.Event) {
		select {
		case got <- e:
		default:
		}
	})

	srv := hook.NewServer(b)
	path, err := srv.Listen(context.Background())
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.Write([]byte("wt-1 tab-1 surf-1 1\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.Close()

	select {
	case ev := <-got:
		busy, ok := ev.(hook.BusyEvent)
		if !ok {
			t.Fatalf("expected BusyEvent, got %T", ev)
		}
		if !busy.Active || busy.WorktreeID != "wt-1" {
			t.Fatalf("unexpected busy event: %+v", busy)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for BusyEvent")
	}
}

func TestServer_NotificationMessage(t *testing.T) {
	b := bus.New()
	defer b.Close()

	got := make(chan any, 1)
	b.Subscribe(func(e bus.Event) {
		select {
		case got <- e:
		default:
		}
	})

	srv := hook.NewServer(b)
	path, err := srv.Listen(context.Background())
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	payload := "wt-1 tab-1 surf-1 claude\n" +
		`{"hook_event_name":"Notification","title":"Permission required","message":"Bash needs approval"}` + "\n"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.Close()

	select {
	case ev := <-got:
		notif, ok := ev.(hook.NotificationEvent)
		if !ok {
			t.Fatalf("expected NotificationEvent, got %T", ev)
		}
		if notif.Title != "Permission required" || notif.Body == "" || notif.Agent != "claude" {
			t.Fatalf("unexpected notification: %+v", notif)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for NotificationEvent")
	}
}
