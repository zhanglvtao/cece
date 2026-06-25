package traecli

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	deviceAuthAAD      = "bearer-device-token-v1"
	deviceAuthBaseDate = "2023-08-20"
	maxIdentityAgeDays = 400
)

// DeviceAuthPayload is the plaintext payload encrypted into Aime's PC LLM token.
type DeviceAuthPayload struct {
	DeviceID  int64  `json:"device_id"`
	Creator   string `json:"creator"`
	LaneID    string `json:"lane_id"`
	SessionID string `json:"session_id"`
	Date      string `json:"date"`
}

// DefaultAimeConfigPath returns the electron-store config path used by Aime.
func DefaultAimeConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Aime", "config.json")
}

// RefreshToken returns a fresh token derived from Aime's cached pcLLMAK payload.
// If the TRAECLI_TOKEN environment variable is set, its value is returned directly.
func RefreshToken() (string, error) {
	if token := os.Getenv("TRAECLI_TOKEN"); token != "" {
		return token, nil
	}
	return RefreshTokenFromAimeConfig(DefaultAimeConfigPath(), time.Now().UTC(), rand.Reader)
}

// RefreshTokenFromAimeConfig reads Aime's cached token, extracts the stable
// identity fields (device_id, creator, lane_id, session_id), and rebuilds a
// token for the provided day.
func RefreshTokenFromAimeConfig(configPath string, now time.Time, nonceReader io.Reader) (string, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read Aime config: %w", err)
	}
	var cfg struct {
		PCLLMAK string `json:"pcLLMAK"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parse Aime config: %w", err)
	}
	if cfg.PCLLMAK == "" {
		return "", fmt.Errorf("Aime config does not contain pcLLMAK")
	}

	payload, err := decodeCachedIdentity(cfg.PCLLMAK, now.UTC())
	if err != nil {
		return "", err
	}
	payload.Date = ""
	return BuildDeviceAuthToken(payload, now.UTC(), nonceReader)
}

// BuildDeviceAuthToken builds an AES-256-GCM device auth token compatible with
// the Aime/traecli llmproxy endpoint.
func BuildDeviceAuthToken(payload DeviceAuthPayload, now time.Time, nonceReader io.Reader) (string, error) {
	date := utcDate(now)
	payload.Date = date
	plain := []byte(fmt.Sprintf(
		`{"device_id":%d,"creator":%s,"lane_id":%s,"session_id":%s,"date":%s}`,
		payload.DeviceID,
		jsonString(payload.Creator),
		jsonString(payload.LaneID),
		jsonString(payload.SessionID),
		jsonString(date),
	))

	gcm, err := gcmForDate(date)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte(deviceAuthAAD))
	token := append(append([]byte{}, nonce...), sealed...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// DecodeDeviceAuthToken decrypts a token generated for the day represented by now.
func DecodeDeviceAuthToken(token string, now time.Time) (DeviceAuthPayload, error) {
	return decodeDeviceAuthTokenForDate(token, utcDate(now))
}

func decodeCachedIdentity(token string, now time.Time) (DeviceAuthPayload, error) {
	start := now.UTC()
	for i := 0; i <= maxIdentityAgeDays; i++ {
		if payload, err := decodeDeviceAuthTokenForDate(token, utcDate(start.AddDate(0, 0, -i))); err == nil {
			return payload, nil
		}
	}
	return DeviceAuthPayload{}, fmt.Errorf("decode cached Aime pcLLMAK: no valid daily key in the last %d days", maxIdentityAgeDays)
}

func decodeDeviceAuthTokenForDate(token, date string) (DeviceAuthPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return DeviceAuthPayload{}, fmt.Errorf("decode token: %w", err)
	}
	gcm, err := gcmForDate(date)
	if err != nil {
		return DeviceAuthPayload{}, err
	}
	if len(raw) < gcm.NonceSize() {
		return DeviceAuthPayload{}, fmt.Errorf("decode token: token too short")
	}
	nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, []byte(deviceAuthAAD))
	if err != nil {
		return DeviceAuthPayload{}, err
	}
	var payload DeviceAuthPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return DeviceAuthPayload{}, fmt.Errorf("parse token payload: %w", err)
	}
	return payload, nil
}

func gcmForDate(date string) (cipher.AEAD, error) {
	key := deriveDeviceAuthDailyKey(date)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func deriveDeviceAuthDailyKey(date string) [32]byte {
	base, _ := time.Parse("2006-01-02", deviceAuthBaseDate)
	cur, _ := time.Parse("2006-01-02", date)
	days := int(cur.Sub(base).Hours() / 24)
	seed := fmt.Sprintf("device-auth-demo|date=%s|days=%d|v=1", date, days)
	return sha256.Sum256([]byte(seed))
}

func utcDate(t time.Time) string { return t.UTC().Format("2006-01-02") }

func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}
