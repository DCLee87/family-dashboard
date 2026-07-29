package app

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func TestPushInputValidation(t *testing.T) {
	publicKey := append([]byte{4}, make([]byte, 64)...)
	if !validVAPIDPublicKey(base64.RawURLEncoding.EncodeToString(publicKey)) {
		t.Fatal("valid VAPID public key was rejected")
	}
	if validVAPIDPublicKey(base64.RawURLEncoding.EncodeToString(make([]byte, 65))) {
		t.Fatal("compressed or invalid VAPID public key was accepted")
	}
	if !validPushKey(base64.RawURLEncoding.EncodeToString(make([]byte, 65)), 65) {
		t.Fatal("valid p256dh key was rejected")
	}
	if !validPushKey(base64.RawURLEncoding.EncodeToString(make([]byte, 16)), 16) {
		t.Fatal("valid auth key was rejected")
	}
	if validPushKey("not-base64url", 16) {
		t.Fatal("invalid push key was accepted")
	}

	validEndpoints := []string{
		"https://push.example/subscription/opaque",
		"https://push.example:8443/value?token=opaque",
	}
	for _, endpoint := range validEndpoints {
		if !validPushEndpoint(endpoint) {
			t.Fatalf("valid endpoint was rejected: %s", endpoint)
		}
	}
	invalidEndpoints := []string{
		"http://push.example/value",
		"https://user:password@push.example/value",
		"https://push.example/value#secret",
		"not-a-url",
	}
	for _, endpoint := range invalidEndpoints {
		if validPushEndpoint(endpoint) {
			t.Fatalf("invalid endpoint was accepted: %s", endpoint)
		}
	}
}

func TestPushDeviceEligibility(t *testing.T) {
	for _, deviceType := range []string{"parent_mobile", "shared_tablet"} {
		if !pushEligibleDevice(database.Device{Type: deviceType}) {
			t.Fatalf("%s should be eligible", deviceType)
		}
	}
	for _, deviceType := range []string{"trusted_pc", "tv"} {
		if pushEligibleDevice(database.Device{Type: deviceType}) {
			t.Fatalf("%s should not be eligible", deviceType)
		}
	}
}

func TestParentMobileCanReplaceAndRevokePushSubscription(t *testing.T) {
	privateKey, publicKey, err := security.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(Config{
		DataDir:          t.TempDir(),
		StartedAt:        time.Now(),
		RecordStart:      true,
		InitialSetupCode: "setup-code",
		SecureCookies:    true,
		VAPIDPublicKey:   publicKey,
		VAPIDPrivateKey:  privateKey,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	if err := application.db.CompleteInitialSetup(ctx, database.InitialSetup{
		CodeHash: security.TokenHash("setup-code"), PINHash: "test-pin-hash",
		DeviceID: "trusted-pc", DeviceName: "Trusted PC",
		AccessID: "pc-access", AccessHash: security.TokenHash("pc-access-token"),
		AccessExpiresAt: now.Add(time.Hour),
		RefreshID:       "pc-refresh", RefreshHash: security.TokenHash("pc-refresh-token"),
		RefreshFamilyID: "pc-family", RefreshExpiresAt: now.Add(time.Hour),
		RecoveryID: "recovery", RecoveryHash: security.TokenHash("recovery-code"),
		Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	codeHash := security.TokenHash("enroll-code")
	claimHash := security.TokenHash("claim-token")
	if err := application.db.CreateEnrollment(
		ctx, "enrollment", codeHash, "parent_mobile", now.Add(time.Hour), now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := application.db.SubmitEnrollment(
		ctx, codeHash, claimHash, "Parent phone", "dad", now,
	); err != nil {
		t.Fatal(err)
	}
	if err := application.db.ApproveEnrollment(ctx, "enrollment", "trusted-pc", now); err != nil {
		t.Fatal(err)
	}
	accessToken := "mobile-access-token"
	if err := application.db.CompleteEnrollment(
		ctx,
		claimHash,
		security.TokenHash("consumed-claim"),
		database.InitialSetup{
			DeviceID: "phone", AccessID: "mobile-access",
			AccessHash: security.TokenHash(accessToken), AccessExpiresAt: now.Add(time.Hour),
			RefreshID: "mobile-refresh", RefreshHash: security.TokenHash("mobile-refresh-token"),
			RefreshFamilyID: "mobile-family", RefreshExpiresAt: now.Add(time.Hour),
			Now: now,
		},
		"dad",
	); err != nil {
		t.Fatal(err)
	}

	configRequest := authenticatedPushRequest(http.MethodGet, "/api/push/config", nil, accessToken)
	configResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(configResponse, configRequest)
	if configResponse.Code != http.StatusOK ||
		!bytes.Contains(configResponse.Body.Bytes(), []byte(`"enabled":true`)) {
		t.Fatalf("push config: status %d, body %s", configResponse.Code, configResponse.Body.String())
	}

	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y))
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	body := bytes.NewBufferString(
		`{"endpoint":"https://push.example/subscription","keys":{"p256dh":"` +
			p256dh + `","auth":"` + auth + `"}}`,
	)
	putRequest := authenticatedPushRequest(
		http.MethodPut, "/api/push/subscription", body, accessToken,
	)
	putResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("push subscribe: status %d, body %s", putResponse.Code, putResponse.Body.String())
	}
	active, err := application.db.PushSubscriptionActive(ctx, "phone")
	if err != nil || !active {
		t.Fatalf("stored push subscription: got %v, err %v", active, err)
	}

	adminToken := "mobile-admin-token"
	csrfToken := "mobile-csrf-token"
	if err := application.db.CreateAdminSession(
		ctx,
		"phone",
		"mobile-admin-session",
		security.TokenHash(adminToken),
		security.TokenHash(csrfToken),
		now.Add(10*time.Minute),
		now.Add(time.Hour),
		now,
	); err != nil {
		t.Fatal(err)
	}
	application.pushClient = pushTestHTTPClient{status: http.StatusCreated}
	testRequest := authenticatedPushRequest(http.MethodPost, "/api/push/test", nil, accessToken)
	testRequest.AddCookie(&http.Cookie{Name: "family_dashboard_admin", Value: adminToken})
	testRequest.Header.Set("X-CSRF-Token", csrfToken)
	testResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(testResponse, testRequest)
	if testResponse.Code != http.StatusOK {
		t.Fatalf("test push: status %d, body %s", testResponse.Code, testResponse.Body.String())
	}

	deleteRequest := authenticatedPushRequest(
		http.MethodDelete, "/api/push/subscription", nil, accessToken,
	)
	deleteResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("push unsubscribe: status %d, body %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	active, err = application.db.PushSubscriptionActive(ctx, "phone")
	if err != nil || active {
		t.Fatalf("revoked push subscription: got %v, err %v", active, err)
	}
}

type pushTestHTTPClient struct {
	status int
}

func (client pushTestHTTPClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: client.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}, nil
}

func authenticatedPushRequest(
	method string,
	path string,
	body *bytes.Buffer,
	accessToken string,
) *http.Request {
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, "https://dashboard.example"+path, nil)
	} else {
		request = httptest.NewRequest(method, "https://dashboard.example"+path, body)
		request.Header.Set("Content-Type", "application/json")
	}
	request.Host = "dashboard.example"
	request.Header.Set("Origin", "https://dashboard.example")
	request.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: accessToken})
	return request
}
