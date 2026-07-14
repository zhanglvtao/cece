package traecli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildDeviceAuthTokenRoundTripsPayload(t *testing.T) {
	nonce := strings.NewReader("123456789012")
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	token, err := BuildDeviceAuthToken(DeviceAuthPayload{
		DeviceID:  265836465,
		Creator:   "zhanglyutao@bytedance.com",
		LaneID:    "",
		SessionID: "f884d313-4ec4-41a8-952f-f941f27e9bc5",
	}, now, nonce)
	if err != nil {
		t.Fatalf("BuildDeviceAuthToken: %v", err)
	}

	decoded, err := DecodeDeviceAuthToken(token, now)
	if err != nil {
		t.Fatalf("DecodeDeviceAuthToken: %v", err)
	}

	if decoded.DeviceID != 265836465 {
		t.Fatalf("DeviceID = %d, want %d", decoded.DeviceID, int64(265836465))
	}
	if decoded.Creator != "zhanglyutao@bytedance.com" {
		t.Fatalf("Creator = %q", decoded.Creator)
	}
	if decoded.SessionID != "f884d313-4ec4-41a8-952f-f941f27e9bc5" {
		t.Fatalf("SessionID = %q", decoded.SessionID)
	}
	if decoded.Date != "2026-06-25" {
		t.Fatalf("Date = %q", decoded.Date)
	}

	if strings.Contains(token, "=") {
		t.Fatalf("token should be unpadded base64url, got %q", token)
	}
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Fatalf("token is not raw base64url: %v", err)
	}
}

func TestRefreshTokenFromAimeConfigRegeneratesStaleCachedPayload(t *testing.T) {
	staleNow := time.Date(2026, 5, 31, 8, 0, 0, 0, time.UTC)
	staleToken, err := BuildDeviceAuthToken(DeviceAuthPayload{
		DeviceID:  265836465,
		Creator:   "zhanglyutao@bytedance.com",
		LaneID:    "",
		SessionID: "f884d313-4ec4-41a8-952f-f941f27e9bc5",
	}, staleNow, strings.NewReader("abcdefghijkl"))
	if err != nil {
		t.Fatalf("BuildDeviceAuthToken stale: %v", err)
	}

	config := map[string]any{
		"pcLLMAK": staleToken,
	}
	raw, _ := json.Marshal(config)
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	refreshed, err := RefreshTokenFromAimeConfig(path, now, strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("RefreshTokenFromAimeConfig: %v", err)
	}

	decoded, err := DecodeDeviceAuthToken(refreshed, now)
	if err != nil {
		t.Fatalf("DecodeDeviceAuthToken refreshed: %v", err)
	}
	if decoded.DeviceID != 265836465 || decoded.Creator != "zhanglyutao@bytedance.com" || decoded.SessionID != "f884d313-4ec4-41a8-952f-f941f27e9bc5" {
		t.Fatalf("decoded refreshed payload = %+v", decoded)
	}
	if decoded.Date != "2026-06-25" {
		t.Fatalf("Date = %q", decoded.Date)
	}
}
