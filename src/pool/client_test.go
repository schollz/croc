package pool

import (
	"net"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestFetchPoolRelaysFiltersReadyAndKeepsUptime(t *testing.T) {
	app := fiber.New()
	app.Get("/relays", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).SendString(`[
			{"address":"198.51.100.10:9009","version":"v1","uptimePercent":87.5,"ready":true},
			{"address":"198.51.100.11:9009","version":"v1","uptimePercent":95.0,"ready":false}
		]`)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create test listener: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = app.Listener(ln)
	}()
	defer app.Shutdown()

	relays, err := FetchPoolRelays("http://"+ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("FetchPoolRelays returned error: %v", err)
	}

	if len(relays) != 1 {
		t.Fatalf("expected 1 ready relay, got %d", len(relays))
	}

	if relays[0].Address != "198.51.100.10:9009" {
		t.Fatalf("unexpected relay address: %s", relays[0].Address)
	}

	if relays[0].GetUptimePercent() != 87.5 {
		t.Fatalf("unexpected uptime percent: got %.1f", relays[0].GetUptimePercent())
	}
}
